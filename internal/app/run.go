package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	bluegreen "github.com/ku9nov/docker-compose-ztd-plugin/internal/blue-green"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/canary"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/docker"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/metricsgate"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/registry"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/rollout"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
)

type Runner struct {
	log *logrus.Logger
}

const autoCleanupLockFileName = ".auto-cleanup.lock"
const envRegistryStrict = "ZTD_REGISTRY_STRICT"

func NewRunner(log *logrus.Logger) *Runner {
	return &Runner{log: log}
}

func (r *Runner) Run(ctx context.Context, cfg cli.Config) error {
	store := state.NewStore(state.DefaultStateDir)
	regStore := registry.NewStore("")
	if cfg.Action == cli.ActionAutoRun {
		return r.runAutoCleanup(ctx, cfg, regStore)
	}

	if err := registerCurrentWorkingDir(regStore); err != nil {
		if registryStrictMode() {
			return fmt.Errorf("failed to register working directory: %w", err)
		}
		r.log.WithError(err).Warn("==> Registry update skipped for current working directory")
	}

	composeAdapter, err := selectComposeAdapter(cfg)
	if err != nil {
		return err
	}

	dockerClient := docker.NewClient(cfg.DockerArgs)
	generator := traefik.NewGenerator(composeAdapter, dockerClient)
	bgDeployer := bluegreen.NewDeployer(r.log, composeAdapter, dockerClient, store)
	canaryDeployer := canary.NewDeployer(r.log, composeAdapter, dockerClient, store)
	cleanupWorker := newCleanupWorker(store, cfg.TraefikConfigFile, bgDeployer, canaryDeployer)
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

func newCleanupWorker(
	store *state.Store,
	traefikConfigFile string,
	bgDeployer *bluegreen.Deployer,
	canaryDeployer *canary.Deployer,
) *state.CleanupWorker {
	return state.NewCleanupWorker(store, func(ctx context.Context, project string, st state.DeploymentState) error {
		switch st.Strategy {
		case state.StrategyBlueGreen:
			return bgDeployer.CleanupProjectState(ctx, project, st, traefikConfigFile)
		case state.StrategyCanary:
			return canaryDeployer.CleanupProjectState(ctx, project, st, traefikConfigFile)
		default:
			return nil
		}
	})
}

func (r *Runner) runAutoCleanup(ctx context.Context, cfg cli.Config, regStore *registry.Store) error {
	entries, err := regStore.List()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if len(entries) == 0 {
		r.log.Info("==> Auto-cleanup: no registered projects found in registry")
		return nil
	}

	composeAdapter, err := selectComposeAdapter(cfg)
	if err != nil {
		return err
	}
	dockerClient := docker.NewClient(cfg.DockerArgs)
	r.log.Infof("==> Running scheduled overdue cleanup across %d registered projects", len(entries))

	var totalScheduledCount int
	var totalDueCount int
	var runErrs []error
	for _, entry := range entries {
		if entry.Disabled {
			r.log.Infof("==> Auto-cleanup: project '%s' is disabled in registry, skipping", entry.WorkingDir)
			continue
		}
		projectDir := entry.WorkingDir
		store := state.NewStore(filepath.Join(projectDir, state.DefaultStateDir))
		bgDeployer := bluegreen.NewDeployer(r.log, composeAdapter, dockerClient, store)
		canaryDeployer := canary.NewDeployer(r.log, composeAdapter, dockerClient, store)

		lockPath := filepath.Join(projectDir, state.DefaultStateDir, autoCleanupLockFileName)
		unlock, acquired, err := state.TryExclusiveFileLock(lockPath)
		if err != nil {
			runErrs = append(runErrs, fmt.Errorf("project %s: lock failed: %w", projectDir, err))
			_ = regStore.SetLastError(projectDir, err)
			continue
		}
		if !acquired {
			r.log.Infof("==> Auto-cleanup: project '%s' is already being processed, skipping", projectDir)
			continue
		}

		var projectScheduledCount int
		var projectDueCount int
		worker := newCleanupWorker(store, resolveTraefikConfigPath(projectDir, cfg.TraefikConfigFile), bgDeployer, canaryDeployer).WithObserver(func(ob state.CleanupObservation) {
			switch ob.Kind {
			case state.CleanupObservationStateLoadError:
				r.log.WithError(ob.Err).Warnf("==> Auto-cleanup: skipped unreadable state for project '%s'", ob.Project)
			case state.CleanupObservationScheduled:
				projectScheduledCount++
				remaining := ob.State.CleanupAt.Sub(ob.Now).Round(time.Second)
				r.log.Infof("==> Auto-cleanup: project '%s' (%s) is scheduled at %s (in %s), not due yet",
					ob.Project,
					ob.State.Service,
					ob.State.CleanupAt.Format(time.RFC3339),
					remaining,
				)
			case state.CleanupObservationDue:
				projectDueCount++
				r.log.Infof("==> Auto-cleanup: project '%s' (%s) is overdue (cleanupAt=%s), processing",
					ob.Project,
					ob.State.Service,
					ob.State.CleanupAt.Format(time.RFC3339),
				)
			case state.CleanupObservationHandled:
				r.log.Infof("==> Auto-cleanup: project '%s' cleanup completed", ob.Project)
			case state.CleanupObservationFailed:
				r.log.WithError(ob.Err).Warnf("==> Auto-cleanup: project '%s' cleanup failed", ob.Project)
			}
		})
		err = worker.ProcessOverdue(ctx)
		if unlockErr := unlock(); unlockErr != nil {
			r.log.WithError(unlockErr).Warnf("==> Auto-cleanup: failed to release lock for project '%s'", projectDir)
		}
		totalScheduledCount += projectScheduledCount
		totalDueCount += projectDueCount
		if err != nil {
			runErrs = append(runErrs, fmt.Errorf("project %s: %w", projectDir, err))
			_ = regStore.SetLastError(projectDir, err)
			continue
		}
		_ = regStore.ClearLastError(projectDir)
	}

	if totalDueCount == 0 && totalScheduledCount == 0 {
		r.log.Info("==> Auto-cleanup: no scheduled cleanups found in state")
	}
	if totalDueCount == 0 && totalScheduledCount > 0 {
		r.log.Infof("==> Auto-cleanup: no overdue cleanups yet; scheduled projects pending=%d", totalScheduledCount)
	}
	return errors.Join(runErrs...)
}

func registerCurrentWorkingDir(store *registry.Store) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, err = store.Register(wd)
	return err
}

func registryStrictMode() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(envRegistryStrict)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func resolveTraefikConfigPath(projectDir string, traefikConfigFile string) string {
	path := strings.TrimSpace(traefikConfigFile)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(projectDir, path)
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
