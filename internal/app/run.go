package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	bluegreen "github.com/ku9nov/docker-compose-ztd-plugin/internal/blue-green"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/canary"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/docker"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/metricsgate"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/rollout"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
)

type Runner struct {
	log *logrus.Logger
}

func NewRunner(log *logrus.Logger) *Runner {
	return &Runner{log: log}
}

func (r *Runner) Run(ctx context.Context, cfg cli.Config) error {
	composeAdapter, err := selectComposeAdapter(cfg)
	if err != nil {
		return err
	}

	dockerClient := docker.NewClient(cfg.DockerArgs)
	generator := traefik.NewGenerator(composeAdapter, dockerClient)
	store := state.NewStore(state.DefaultStateDir)
	bgDeployer := bluegreen.NewDeployer(r.log, composeAdapter, dockerClient, store)
	canaryDeployer := canary.NewDeployer(r.log, composeAdapter, dockerClient, store)

	cleanupWorker := state.NewCleanupWorker(store, func(ctx context.Context, project string, st state.DeploymentState) error {
		switch st.Strategy {
		case state.StrategyBlueGreen:
			return bgDeployer.CleanupProjectState(ctx, project, st, cfg.TraefikConfigFile)
		case state.StrategyCanary:
			return canaryDeployer.CleanupProjectState(ctx, project, st, cfg.TraefikConfigFile)
		default:
			return nil
		}
	})
	if err := cleanupWorker.ProcessOverdue(ctx); err != nil {
		r.log.WithError(err).Warn("==> Failed to process overdue scheduled cleanups")
	}

	if cfg.Service == "up" {
		composeServices, err := collectComposeServices(cfg.ComposeFiles)
		if err != nil {
			return fmt.Errorf("failed to read compose services: %w", err)
		}
		if deleted, err := store.DeleteByServiceNames(composeServices); err != nil {
			return fmt.Errorf("failed to clean service states before up: %w", err)
		} else if deleted > 0 {
			r.log.WithField("statesDeleted", deleted).Info("==> Removed stale service state files before up.")
		}

		r.log.Info("==> Bringing up services.")
		if err := composeAdapter.Up(ctx, cfg.ComposeFiles, cfg.EnvFiles, "", cfg.UpDetached, false); err != nil {
			return err
		}

		time.Sleep(5 * time.Second)
		if err := generator.Generate(ctx, cfg.ComposeFiles, cfg.EnvFiles, cfg.TraefikConfigFile); err != nil {
			return err
		}
		if !cfg.UpDetached {
			return composeAdapter.LogsFollowTail(ctx, cfg.ComposeFiles, "", 1)
		}
		return nil
	}

	switch cfg.Strategy {
	case cli.StrategyRolling:
		updater := rollout.NewUpdater(r.log, composeAdapter, dockerClient, generator)
		return updater.Run(ctx, rollout.Options{
			Service:              cfg.Service,
			ComposeFiles:         cfg.ComposeFiles,
			EnvFiles:             cfg.EnvFiles,
			HealthcheckTimeout:   cfg.HealthcheckTimeout,
			NoHealthcheckTimeout: cfg.NoHealthcheckTimeout,
			WaitAfterHealthy:     cfg.WaitAfterHealthy,
			ProxyType:            cfg.ProxyType,
			TraefikConfigFile:    cfg.TraefikConfigFile,
		})
	case cli.StrategyBlueGreen:
		return bgDeployer.Run(ctx, bluegreen.Options{
			Service:           cfg.Service,
			Action:            cfg.Action,
			SwitchTo:          cfg.SwitchTo,
			ComposeFiles:      cfg.ComposeFiles,
			EnvFiles:          cfg.EnvFiles,
			TraefikConfigFile: cfg.TraefikConfigFile,
			HostMode:          cfg.HostMode,
			HeadersMode:       cfg.HeadersMode,
			CookiesMode:       cfg.CookiesMode,
			IPMode:            cfg.IPMode,
			AutoCleanup:       cfg.AutoCleanup,
			HealthTimeout:     cfg.HealthcheckTimeout,
			NoHealthTimeout:   cfg.NoHealthcheckTimeout,
			WaitAfterHealthy:  cfg.WaitAfterHealthy,
			Metrics: metricsgate.Config{
				Enabled:          cfg.Analyze,
				URL:              cfg.MetricsURL,
				Window:           cfg.AnalyzeWindow,
				Interval:         cfg.AnalyzeInterval,
				MinRequests:      cfg.AnalyzeMinRequests,
				Max5xxRatio:      cfg.AnalyzeMax5xxRatio,
				Max4xxRatio:      cfg.AnalyzeMax4xxRatio,
				MaxMeanLatencyMS: cfg.AnalyzeMaxLatencyMS,
			},
		})
	case cli.StrategyCanary:
		return canaryDeployer.Run(ctx, canary.Options{
			Service:           cfg.Service,
			Action:            cfg.Action,
			ComposeFiles:      cfg.ComposeFiles,
			EnvFiles:          cfg.EnvFiles,
			Weight:            cfg.Weight,
			TraefikConfigFile: cfg.TraefikConfigFile,
			AutoCleanup:       cfg.AutoCleanup,
			HealthTimeout:     cfg.HealthcheckTimeout,
			NoHealthTimeout:   cfg.NoHealthcheckTimeout,
			WaitAfterHealthy:  cfg.WaitAfterHealthy,
			Metrics: metricsgate.Config{
				Enabled:          cfg.Analyze,
				URL:              cfg.MetricsURL,
				Window:           cfg.AnalyzeWindow,
				Interval:         cfg.AnalyzeInterval,
				MinRequests:      cfg.AnalyzeMinRequests,
				Max5xxRatio:      cfg.AnalyzeMax5xxRatio,
				Max4xxRatio:      cfg.AnalyzeMax4xxRatio,
				MaxMeanLatencyMS: cfg.AnalyzeMaxLatencyMS,
			},
		})
	default:
		return fmt.Errorf("unsupported strategy: %s", cfg.Strategy)
	}
}

type composeServicesFile struct {
	Services map[string]any `yaml:"services"`
}

func collectComposeServices(files []string) ([]string, error) {
	services := map[string]struct{}{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}

		var cfg composeServicesFile
		if err := configio.UnmarshalYAML(data, &cfg); err != nil {
			return nil, err
		}

		for service := range cfg.Services {
			services[service] = struct{}{}
		}
	}

	out := make([]string, 0, len(services))
	for service := range services {
		out = append(out, service)
	}
	sort.Strings(out)
	return out, nil
}

func selectComposeAdapter(cfg cli.Config) (compose.Adapter, error) {
	if os.Getenv("ZTD_COMPOSE_ADAPTER") == "api" {
		return compose.NewAPIAdapter(), nil
	}

	adapter, err := compose.NewShellAdapter(cfg.DockerArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize compose adapter: %w", err)
	}
	return adapter, nil
}
