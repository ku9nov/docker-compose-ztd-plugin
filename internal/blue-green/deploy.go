package bluegreen

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/metricsgate"
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
	Metrics           metricsgate.Config
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
		if opt.Metrics.Enabled {
			d.log.Infof("==> Blue-green cycle is already active for service '%s' (key: %s). Running analysis-only for existing green.", opt.Service, project)
			if d.runLifetimeGreenMetricsGate(ctx, opt, existingState, "observe") {
				return nil
			}
			d.runMetricsGate(ctx, opt, blueGreenMetricServiceName(opt.Service, state.ColorGreen), "observe")
			return nil
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
	if baseline, baselineErr := d.captureServiceSnapshot(ctx, opt, blueGreenMetricServiceName(opt.Service, state.ColorGreen)); baselineErr != nil {
		d.log.Warnf("==> Unable to capture green baseline metrics: %v", baselineErr)
	} else {
		currentState.GreenStats = &state.MetricsBaseline{
			CapturedAt:    time.Now().UTC(),
			Requests2xx:   baseline.Requests2xx,
			Requests4xx:   baseline.Requests4xx,
			Requests5xx:   baseline.Requests5xx,
			DurationSum:   baseline.DurationSum,
			DurationCount: baseline.DurationCount,
		}
		if saveErr := d.store.Save(stateKey, currentState); saveErr != nil {
			d.log.Warnf("==> Unable to persist green baseline metrics: %v", saveErr)
		}
	}
	d.runMetricsGate(ctx, opt, blueGreenMetricServiceName(opt.Service, state.ColorGreen), "deploy")

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

func (d *Deployer) runMetricsGate(ctx context.Context, opt Options, targetService string, stage string) {
	if !opt.Metrics.Enabled {
		return
	}

	d.log.Infof("==> Metrics analysis started (blue-green stage=%s, window=%s, interval=%s)", stage, opt.Metrics.Window, opt.Metrics.Interval)
	result, err := metricsgate.AnalyzeWithProgress(
		ctx,
		&http.Client{Timeout: 10 * time.Second},
		opt.Metrics,
		[]string{targetService},
		func(p metricsgate.Progress) {
			elapsed := p.Elapsed
			if elapsed > p.Window {
				elapsed = p.Window
			}
			percent := int(float64(elapsed) / float64(p.Window) * 100)
			d.log.Infof("==> Metrics analysis progress: %d%% (%s/%s), samples=%d, pollErrors=%d", percent, elapsed.Truncate(time.Second), p.Window, p.SuccessfulSamples, p.FailedPolls)
		},
	)
	if err != nil {
		d.log.Warnf("==> Blue-green metrics gate failed to run after %s: %v", stage, err)
		return
	}
	switch result.Verdict {
	case metricsgate.VerdictPass:
		d.log.Infof("%s", formatBlueGreenResultReport(result, "OK -> keep current active color"))
	case metricsgate.VerdictInsufficientData:
		d.log.Warnf("%s", formatBlueGreenResultReport(result, "WARN -> not enough traffic for strict decision"))
	default:
		d.log.Warnf("%s", formatBlueGreenResultReport(result, "WARN -> manual decision required"))
	}
}

func blueGreenMetricServiceName(service string, color string) string {
	return service + "-" + color
}

func (d *Deployer) runLifetimeGreenMetricsGate(ctx context.Context, opt Options, st state.DeploymentState, stage string) bool {
	if st.GreenStats == nil {
		d.log.Warn("==> Green lifetime baseline is missing; falling back to window analysis")
		return false
	}
	targetService := blueGreenMetricServiceName(st.Service, state.ColorGreen)
	current, err := d.captureServiceSnapshot(ctx, opt, targetService)
	if err != nil {
		d.log.Warnf("==> Unable to capture current green metrics snapshot: %v", err)
		return false
	}
	start := metricsgate.Snapshot{
		Requests2xx:   st.GreenStats.Requests2xx,
		Requests4xx:   st.GreenStats.Requests4xx,
		Requests5xx:   st.GreenStats.Requests5xx,
		DurationSum:   st.GreenStats.DurationSum,
		DurationCount: st.GreenStats.DurationCount,
	}
	end := metricsgate.Snapshot{
		Requests2xx:   current.Requests2xx,
		Requests4xx:   current.Requests4xx,
		Requests5xx:   current.Requests5xx,
		DurationSum:   current.DurationSum,
		DurationCount: current.DurationCount,
	}
	if snapshotWentBackwards(start, end) {
		d.log.Warn("==> Green lifetime baseline appears reset (Traefik counters moved backwards); falling back to window analysis")
		return false
	}

	d.log.Infof("==> Lifetime metrics analysis started (blue-green stage=%s, since=%s)", stage, st.GreenStats.CapturedAt.Format(time.RFC3339))
	result := metricsgate.EvaluateFromSnapshots(opt.Metrics, start, end, 2)
	switch result.Verdict {
	case metricsgate.VerdictPass:
		d.log.Infof("%s", formatBlueGreenResultReport(result, "OK -> keep current active color"))
	case metricsgate.VerdictInsufficientData:
		d.log.Warnf("%s", formatBlueGreenResultReport(result, "WARN -> not enough traffic for strict decision"))
	default:
		d.log.Warnf("%s", formatBlueGreenResultReport(result, "WARN -> manual decision required"))
	}
	return true
}

func (d *Deployer) captureServiceSnapshot(ctx context.Context, opt Options, targetService string) (*metricsgate.Snapshot, error) {
	s, err := metricsgate.CaptureSnapshot(ctx, &http.Client{Timeout: 10 * time.Second}, opt.Metrics.URL, []string{targetService})
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func snapshotWentBackwards(start metricsgate.Snapshot, end metricsgate.Snapshot) bool {
	return end.Requests2xx < start.Requests2xx ||
		end.Requests4xx < start.Requests4xx ||
		end.Requests5xx < start.Requests5xx
}

func formatBlueGreenResultReport(result metricsgate.Result, decision string) string {
	status := strings.ToUpper(string(result.Verdict))
	reasonLine := ""
	if len(result.Reasons) > 0 {
		reasonLine = fmt.Sprintf("\nReason: %s", strings.Join(result.Reasons, "; "))
	}

	return fmt.Sprintf(
		"==> Deployment Result (blue-green)\nStatus: %s%s\nTraffic: %.0f requests (%d samples)\n\nNew Version:\n  2xx: %.2f%%\n  4xx: %.2f%%\n  5xx: %.2f%%\n  Latency: %.2fms\n\nDecision: %s",
		status,
		reasonLine,
		result.TotalRequests,
		result.Samples,
		result.Ratio2xx*100,
		result.Ratio4xx*100,
		result.Ratio5xx*100,
		result.MeanLatencyMS,
		decision,
	)
}
