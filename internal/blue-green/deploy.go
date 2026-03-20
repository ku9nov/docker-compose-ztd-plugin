package bluegreen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/safeguard"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

type Options struct {
	Service           string
	Action            string
	SwitchTo          string
	ComposeFiles      []string
	EnvFiles          []string
	TraefikConfigFile string
	HostMode          string
	HeadersMode       string
	CookiesMode       string
	IPMode            string
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

func (d *Deployer) Run(ctx context.Context, opt Options) (err error) {
	switch opt.Action {
	case "":
		return d.deploy(ctx, opt)
	case "switch":
		return d.switchTraffic(ctx, opt)
	case "cleanup":
		return d.cleanupInactive(ctx, opt)
	default:
		return fmt.Errorf("unsupported blue-green action: %s", opt.Action)
	}
}

func (d *Deployer) deploy(ctx context.Context, opt Options) (err error) {
	project, existingState, err := d.findStateByService(opt.Service)
	if err == nil {
		if err := d.validateStateContainersArePresent(ctx, opt, existingState); err != nil {
			return err
		}
		return fmt.Errorf("blue-green state for service %s already exists (key: %s); finish current cycle with switch/cleanup before starting a new deploy", opt.Service, project)
	}
	if err != nil && !errors.Is(err, errBlueGreenStateNotFound) {
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
	d.log.Infof("==> Blue-green deploy: scaling '%s' to %d instances", opt.Service, target)
	if err := d.compose.Scale(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service, target); err != nil {
		return err
	}

	var newIDs []string
	guard := safeguard.NewRollbackGuard(d.log, "blue-green post-scale rollback", func(ctx context.Context) error {
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
		d.log.Infof("==> Waiting for green containers to be healthy (timeout: %d seconds)", opt.HealthTimeout)
		ok, err := d.waitHealthy(ctx, newIDs, len(oldIDs), opt.HealthTimeout)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("green containers are not healthy")
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
		Strategy:  state.StrategyBlueGreen,
		Blue:      oldIDs,
		Green:     newIDs,
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
		QA: &state.QAModes{
			Host:    opt.HostMode,
			Headers: opt.HeadersMode,
			Cookies: opt.CookiesMode,
			IP:      opt.IPMode,
		},
	}
	if err := d.store.Save(stateKey, currentState); err != nil {
		return err
	}

	if err := traefik.ApplyBlueGreenConfig(opt.TraefikConfigFile, traefik.BlueGreenConfigInput{
		Service:        opt.Service,
		Active:         state.ColorBlue,
		ProductionRule: productionRule,
		Port:           port,
		BlueIDs:        oldIDs,
		GreenIDs:       newIDs,
		QA:             currentState.QA,
		HealthCheck:    hc,
	}); err != nil {
		return err
	}

	guard.Disarm()
	d.log.Infof("==> Blue-green deploy ready. Blue active, green waiting for switch.")
	return nil
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

func (d *Deployer) validateStateContainersArePresent(ctx context.Context, opt Options, st state.DeploymentState) error {
	currentIDs, err := d.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, st.Service)
	if err != nil {
		return err
	}
	currentSet := map[string]struct{}{}
	for _, id := range currentIDs {
		currentSet[id] = struct{}{}
	}

	missingBlue := missingIDs(st.Blue, currentSet)
	missingGreen := missingIDs(st.Green, currentSet)
	if len(missingBlue) == 0 && len(missingGreen) == 0 {
		return nil
	}
	return fmt.Errorf("blue-green state drift detected for service %s: missing blue=%v green=%v; run cleanup and start a fresh cycle", st.Service, missingBlue, missingGreen)
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
