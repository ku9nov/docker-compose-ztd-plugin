package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/docker"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/rollout"
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

	if cfg.Service == "up" {
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
