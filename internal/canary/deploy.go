package canary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/safeguard"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
	"github.com/sirupsen/logrus"
)

var errCanaryStateNotFound = errors.New("canary state not found")

type Options struct {
	Service           string
	Action            string
	ComposeFiles      []string
	EnvFiles          []string
	Weight            int
	TraefikConfigFile string
	AutoCleanup       time.Duration
	HealthTimeout     int
	NoHealthTimeout   int
	WaitAfterHealthy  int
}

type dockerOps interface {
	HasHealthcheck(ctx context.Context, containerID string) (bool, error)
	HealthStatus(ctx context.Context, containerID string) (string, error)
	Stop(ctx context.Context, containerIDs []string) error
	Remove(ctx context.Context, containerIDs []string) error
	Labels(ctx context.Context, containerID string) (map[string]string, error)
}

type Deployer struct {
	log     *logrus.Logger
	compose compose.Adapter
	docker  dockerOps
	store   *state.Store
}

func NewDeployer(log *logrus.Logger, composeAdapter compose.Adapter, dockerClient dockerOps, store *state.Store) *Deployer {
	return &Deployer{
		log:     log,
		compose: composeAdapter,
		docker:  dockerClient,
		store:   store,
	}
}

func (d *Deployer) Run(ctx context.Context, opt Options) error {
	switch opt.Action {
	case "":
		return d.deploy(ctx, opt)
	case "promote":
		return d.promote(ctx, opt)
	case "rollback":
		return d.rollback(ctx, opt)
	case "cleanup":
		return d.cleanup(ctx, opt)
	default:
		return fmt.Errorf("unsupported canary action: %s", opt.Action)
	}
}

func (d *Deployer) deploy(ctx context.Context, opt Options) (err error) {
	project, existingState, err := d.findStateByService(opt.Service)
	if err == nil {
		return d.deployFromExistingState(ctx, opt, project, existingState)
	}
	if err != nil && !errors.Is(err, errCanaryStateNotFound) {
		return err
	}

	oldIDs, err := d.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service)
	if err != nil {
		return err
	}
	if len(oldIDs) == 0 {
		d.log.Infof("==> Service '%s' is not running. Starting the service.", opt.Service)
		if err := d.compose.Up(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service, true, true); err != nil {
			return err
		}
		oldIDs, err = d.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service)
		if err != nil {
			return err
		}
		if len(oldIDs) == 0 {
			return fmt.Errorf("service %s started but no containers found", opt.Service)
		}
	}

	target := len(oldIDs) * 2
	d.log.Infof("==> Canary deploy: scaling '%s' to %d instances", opt.Service, target)
	if err := d.compose.Scale(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service, target); err != nil {
		return err
	}

	var newIDs []string
	guard := safeguard.NewRollbackGuard(d.log, "canary post-scale rollback", func(ctx context.Context) error {
		if len(newIDs) == 0 {
			return nil
		}
		stopErr := d.docker.Stop(ctx, newIDs)
		rmErr := d.docker.Remove(ctx, newIDs)
		return safeguard.WrapErrors("cleanup new containers", stopErr, rmErr)
	})
	defer guard.Run(ctx, &err)

	allIDs, err := d.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service)
	if err != nil {
		return err
	}
	newIDs = diffIDs(oldIDs, allIDs)
	if len(newIDs) == 0 {
		return fmt.Errorf("could not find new containers for service %s", opt.Service)
	}

	hasHC, err := d.docker.HasHealthcheck(ctx, oldIDs[0])
	if err != nil {
		return err
	}
	if hasHC {
		d.log.Infof("==> Waiting for canary containers to be healthy (timeout: %d seconds)", opt.HealthTimeout)
		ok, err := d.waitHealthy(ctx, newIDs, len(oldIDs), opt.HealthTimeout)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("canary containers are not healthy")
		}
		if opt.WaitAfterHealthy > 0 {
			time.Sleep(time.Duration(opt.WaitAfterHealthy) * time.Second)
		}
	} else if opt.NoHealthTimeout > 0 {
		time.Sleep(time.Duration(opt.NoHealthTimeout) * time.Second)
	}

	labels, err := d.docker.Labels(ctx, oldIDs[0])
	if err != nil {
		return err
	}
	project, err = state.ResolveProjectName(labels, os.Getenv("COMPOSE_PROJECT_NAME"))
	if err != nil {
		return err
	}
	stateKey, err := state.ServiceStateKey(project, opt.Service)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, opt.Service)
	hc := extractHealthCheck(labels, opt.Service)

	currentState := state.DeploymentState{
		Service:   opt.Service,
		Strategy:  state.StrategyCanary,
		Old:       oldIDs,
		New:       newIDs,
		Weight:    opt.Weight,
		CreatedAt: time.Now().UTC(),
	}
	if err := d.store.Save(stateKey, currentState); err != nil {
		return err
	}
	if err := traefik.ApplyCanaryConfig(opt.TraefikConfigFile, traefik.CanaryConfigInput{
		Service:        opt.Service,
		ProductionRule: productionRule,
		Port:           port,
		OldIDs:         oldIDs,
		NewIDs:         newIDs,
		NewWeight:      opt.Weight,
		HealthCheck:    hc,
	}); err != nil {
		return err
	}

	guard.Disarm()
	d.log.Infof("==> Canary deploy ready. old=%d%% new=%d%%", 100-opt.Weight, opt.Weight)
	return nil
}

func (d *Deployer) deployFromExistingState(ctx context.Context, opt Options, project string, st state.DeploymentState) error {
	if err := d.validateStateContainersArePresent(ctx, opt, st); err != nil {
		return err
	}

	labels, err := d.labelsFromState(ctx, st)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, st.Service)
	hc := extractHealthCheck(labels, st.Service)

	if err := traefik.ApplyCanaryConfig(opt.TraefikConfigFile, traefik.CanaryConfigInput{
		Service:        st.Service,
		ProductionRule: productionRule,
		Port:           port,
		OldIDs:         st.Old,
		NewIDs:         st.New,
		NewWeight:      opt.Weight,
		HealthCheck:    hc,
	}); err != nil {
		return err
	}

	now := time.Now().UTC()
	st.Weight = opt.Weight
	st.SwitchedAt = &now
	st.CleanupAt = nil
	if err := d.store.Save(project, st); err != nil {
		return err
	}

	d.log.Infof("==> Canary deploy reused existing pool without scaling. old=%d%% new=%d%%", 100-opt.Weight, opt.Weight)
	return nil
}

func (d *Deployer) promote(ctx context.Context, opt Options) error {
	if opt.Weight <= 0 || opt.Weight > 100 {
		return fmt.Errorf("promote weight must be between 1 and 100")
	}
	if opt.AutoCleanup > 0 && opt.Weight != 100 {
		return fmt.Errorf("--auto-cleanup for promote requires --weight=100")
	}

	project, st, err := d.findStateByService(opt.Service)
	if err != nil {
		return err
	}
	labels, err := d.labelsFromState(ctx, st)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, st.Service)
	hc := extractHealthCheck(labels, st.Service)

	if err := traefik.ApplyCanaryConfig(opt.TraefikConfigFile, traefik.CanaryConfigInput{
		Service:        st.Service,
		ProductionRule: productionRule,
		Port:           port,
		OldIDs:         st.Old,
		NewIDs:         st.New,
		NewWeight:      opt.Weight,
		HealthCheck:    hc,
	}); err != nil {
		return err
	}

	now := time.Now().UTC()
	st.Weight = opt.Weight
	st.SwitchedAt = &now
	if opt.AutoCleanup > 0 {
		cleanupAt := now.Add(opt.AutoCleanup)
		st.CleanupAt = &cleanupAt
	} else {
		st.CleanupAt = nil
	}
	if err := d.store.Save(project, st); err != nil {
		return err
	}
	d.log.Infof("==> Canary promote updated to new=%d%%", opt.Weight)
	return nil
}

func (d *Deployer) rollback(ctx context.Context, opt Options) error {
	project, st, err := d.findStateByService(opt.Service)
	if err != nil {
		return err
	}
	labels, err := d.labelsFromState(ctx, st)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, st.Service)
	hc := extractHealthCheck(labels, st.Service)

	if err := traefik.ApplyCanaryConfig(opt.TraefikConfigFile, traefik.CanaryConfigInput{
		Service:        st.Service,
		ProductionRule: productionRule,
		Port:           port,
		OldIDs:         st.Old,
		NewIDs:         st.New,
		NewWeight:      0,
		HealthCheck:    hc,
	}); err != nil {
		return err
	}

	now := time.Now().UTC()
	st.Weight = 0
	st.SwitchedAt = &now
	if opt.AutoCleanup > 0 {
		cleanupAt := now.Add(opt.AutoCleanup)
		st.CleanupAt = &cleanupAt
	} else {
		st.CleanupAt = nil
	}
	if err := d.store.Save(project, st); err != nil {
		return err
	}
	d.log.Infof("==> Canary rollback completed. new=0%%")
	return nil
}

func (d *Deployer) cleanup(ctx context.Context, opt Options) error {
	project, st, err := d.findStateByService(opt.Service)
	if err != nil {
		return err
	}
	return d.CleanupProjectState(ctx, project, st, opt.TraefikConfigFile)
}

func (d *Deployer) CleanupProjectState(ctx context.Context, project string, st state.DeploymentState, traefikConfigFile string) error {
	var inactive []string
	var active []string
	inactiveSide := "old"
	weight := st.Weight
	switch {
	case st.Weight == 100:
		inactive = append(inactive, st.Old...)
		active = append(active, st.New...)
		inactiveSide = "old"
	case st.Weight == 0:
		inactive = append(inactive, st.New...)
		active = append(active, st.Old...)
		inactiveSide = "new"
	default:
		return fmt.Errorf("cleanup requires terminal canary state (new=0 or new=100), current new=%d", st.Weight)
	}

	d.log.Infof("==> Cleanup requested for canary service '%s': new=%d%%, removing inactive=%s", st.Service, st.Weight, inactiveSide)
	if len(inactive) > 0 {
		d.log.Infof("==> Stopping inactive %s containers for service '%s': %v", inactiveSide, st.Service, inactive)
		if err := d.docker.Stop(ctx, inactive); err != nil {
			return err
		}
		d.log.Infof("==> Removing inactive %s containers for service '%s': %v", inactiveSide, st.Service, inactive)
		if err := d.docker.Remove(ctx, inactive); err != nil {
			return err
		}
	} else {
		d.log.Infof("==> No inactive %s containers found for service '%s'", inactiveSide, st.Service)
	}

	labels, err := d.labelsFromCandidates(ctx, active, inactive)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, st.Service)
	hc := extractHealthCheck(labels, st.Service)

	var oldIDs []string
	var newIDs []string
	if weight == 100 {
		newIDs = active
	} else {
		oldIDs = active
	}

	if err := traefik.ApplyCanaryConfig(traefikConfigFile, traefik.CanaryConfigInput{
		Service:        st.Service,
		ProductionRule: productionRule,
		Port:           port,
		OldIDs:         oldIDs,
		NewIDs:         newIDs,
		NewWeight:      weight,
		HealthCheck:    hc,
	}); err != nil {
		return fmt.Errorf("failed to update traefik config after cleanup: %w", err)
	}

	if err := d.store.Delete(project); err != nil {
		return err
	}
	d.log.Infof("==> Canary state file removed for key '%s'", project)
	return nil
}

func (d *Deployer) findStateByService(service string) (string, state.DeploymentState, error) {
	projects, err := d.store.ListProjects()
	if err != nil {
		return "", state.DeploymentState{}, err
	}
	var matchedProject string
	var matchedState state.DeploymentState
	for _, project := range projects {
		st, err := d.store.Load(project)
		if err != nil {
			continue
		}
		if st.Service != service || st.Strategy != state.StrategyCanary {
			continue
		}
		if matchedProject != "" {
			return "", state.DeploymentState{}, fmt.Errorf("multiple canary states found for service %s", service)
		}
		matchedProject = project
		matchedState = st
	}
	if matchedProject == "" {
		return "", state.DeploymentState{}, fmt.Errorf("%w for service %s", errCanaryStateNotFound, service)
	}
	return matchedProject, matchedState, nil
}

func (d *Deployer) validateStateContainersArePresent(ctx context.Context, opt Options, st state.DeploymentState) error {
	currentIDs, err := d.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, st.Service)
	if err != nil {
		return err
	}
	currentSet := map[string]struct{}{}
	for _, id := range currentIDs {
		currentSet[id] = struct{}{}
	}

	missingOld := missingIDs(st.Old, currentSet)
	missingNew := missingIDs(st.New, currentSet)
	if len(missingOld) == 0 && len(missingNew) == 0 {
		return nil
	}
	return fmt.Errorf("canary state drift detected for service %s: missing old=%v new=%v; run cleanup and start a fresh cycle", st.Service, missingOld, missingNew)
}

func (d *Deployer) labelsFromState(ctx context.Context, st state.DeploymentState) (map[string]string, error) {
	return d.labelsFromCandidates(ctx, append([]string{}, st.Old...), st.New)
}

func (d *Deployer) labelsFromCandidates(ctx context.Context, first []string, fallback []string) (map[string]string, error) {
	candidates := append([]string{}, first...)
	candidates = append(candidates, fallback...)
	for _, id := range candidates {
		labels, err := d.docker.Labels(ctx, id)
		if err == nil && len(labels) > 0 {
			return labels, nil
		}
	}
	return nil, fmt.Errorf("unable to read labels from canary containers")
}

func productionRuleAndPort(labels map[string]string, service string) (string, string) {
	rule := labels["traefik.http.routers."+service+".rule"]
	if strings.TrimSpace(rule) == "" {
		rule = fmt.Sprintf("Host(`%s.local`)", service)
	}
	port := labels["traefik.http.services."+service+".loadbalancer.server.port"]
	if strings.TrimSpace(port) == "" {
		port = "80"
	}
	return rule, port
}

func extractHealthCheck(labels map[string]string, serviceName string) *types.HealthChecks {
	prefix := "traefik.http.services." + serviceName + ".loadbalancer.healthCheck."
	hc := &types.HealthChecks{
		Path:            labels[prefix+"path"],
		Interval:        labels[prefix+"interval"],
		Timeout:         labels[prefix+"timeout"],
		Scheme:          labels[prefix+"scheme"],
		Mode:            labels[prefix+"mode"],
		Hostname:        labels[prefix+"hostname"],
		Port:            labels[prefix+"port"],
		FollowRedirects: labels[prefix+"followRedirects"],
		Method:          labels[prefix+"method"],
		Status:          labels[prefix+"status"],
	}

	headers := map[string]string{}
	for k, v := range labels {
		if strings.HasPrefix(k, prefix+"headers.") {
			headers[strings.TrimPrefix(k, prefix+"headers.")] = v
		}
	}
	if len(headers) > 0 {
		hc.Headers = headers
	}

	if hc.Path == "" && hc.Interval == "" && hc.Timeout == "" && hc.Scheme == "" && hc.Mode == "" &&
		hc.Hostname == "" && hc.Port == "" && hc.FollowRedirects == "" && hc.Method == "" && hc.Status == "" && len(hc.Headers) == 0 {
		return nil
	}
	return hc
}

func (d *Deployer) waitHealthy(ctx context.Context, containerIDs []string, expected int, timeoutSec int) (bool, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		okCount := 0
		for _, id := range containerIDs {
			status, err := d.docker.HealthStatus(ctx, id)
			if err != nil {
				return false, err
			}
			if status == "healthy" {
				okCount++
			}
		}
		if okCount == expected {
			return true, nil
		}
		time.Sleep(time.Second)
	}
	return false, nil
}

func diffIDs(oldIDs []string, allIDs []string) []string {
	old := map[string]struct{}{}
	for _, id := range oldIDs {
		old[id] = struct{}{}
	}
	var out []string
	for _, id := range allIDs {
		if _, exists := old[id]; !exists {
			out = append(out, id)
		}
	}
	return out
}

func missingIDs(expected []string, current map[string]struct{}) []string {
	out := make([]string, 0)
	for _, id := range expected {
		if _, ok := current[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}
