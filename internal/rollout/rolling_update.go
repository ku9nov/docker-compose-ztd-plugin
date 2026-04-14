package rollout

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/healthdiag"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/safeguard"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
)

type Options struct {
	Service              string
	ComposeFiles         []string
	EnvFiles             []string
	HealthcheckTimeout   int
	NoHealthcheckTimeout int
	WaitAfterHealthy     int
	ProxyType            string
	TraefikConfigFile    string
}

type Updater struct {
	log       *logrus.Logger
	compose   compose.Adapter
	docker    dockerOps
	generator generatorOps
}

type dockerOps interface {
	HasHealthcheck(ctx context.Context, containerID string) (bool, error)
	HealthStatus(ctx context.Context, containerID string) (string, error)
	LogsTail(ctx context.Context, containerID string, tail int) (string, error)
	Stop(ctx context.Context, containerIDs []string) error
	Remove(ctx context.Context, containerIDs []string) error
}

type generatorOps interface {
	Generate(ctx context.Context, composeFiles []string, envFiles []string, outputPath string) error
}

func NewUpdater(log *logrus.Logger, composeAdapter compose.Adapter, dockerClient dockerOps, generator generatorOps) *Updater {
	return &Updater{
		log:       log,
		compose:   composeAdapter,
		docker:    dockerClient,
		generator: generator,
	}
}

func (u *Updater) Run(ctx context.Context, opt Options) (err error) {
	if err := validateProxyType(opt.ProxyType); err != nil {
		return err
	}

	oldIDs, err := u.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service)
	if err != nil {
		return err
	}
	if len(oldIDs) == 0 {
		u.log.Infof("==> Service '%s' is not running. Starting the service.", opt.Service)
		return u.compose.Up(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service, true, true)
	}

	scale := len(oldIDs)
	target := scale * 2
	u.log.Infof("==> Scaling '%s' to '%d' instances", opt.Service, target)
	if err := u.compose.Scale(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service, target); err != nil {
		return err
	}
	newIDs := []string{}
	guard := safeguard.NewRollbackGuard(u.log, "post-scale rollback", func(ctx context.Context) error {
		if len(newIDs) == 0 {
			return nil
		}
		u.log.Warnf("==> Cleaning up new containers: %v", newIDs)
		stopErr := u.docker.Stop(ctx, newIDs)
		rmErr := u.docker.Remove(ctx, newIDs)
		return safeguard.WrapErrors("cleanup new containers", stopErr, rmErr)
	})
	defer guard.Run(ctx, &err)

	allIDs, err := u.compose.PsQuiet(ctx, opt.ComposeFiles, opt.EnvFiles, opt.Service)
	if err != nil {
		return err
	}
	newIDs = diffIDs(oldIDs, allIDs)
	if len(newIDs) == 0 {
		return fmt.Errorf("could not find new containers for service %s", opt.Service)
	}

	hasHC, err := u.docker.HasHealthcheck(ctx, oldIDs[0])
	if err != nil {
		return err
	}

	if hasHC {
		u.log.Infof("==> Waiting for new containers to be healthy (timeout: %d seconds)", opt.HealthcheckTimeout)
		ok, err := u.waitHealthy(ctx, newIDs, scale, opt.HealthcheckTimeout)
		if err != nil {
			return err
		}
		if !ok {
			u.log.Error("==> New containers are not healthy. Rolling back.")
			healthdiag.LogUnhealthyContainerLogs(ctx, u.log, u.docker, newIDs, 20)
			_ = u.docker.Stop(ctx, newIDs)
			_ = u.docker.Remove(ctx, newIDs)
			guard.Disarm()
			return fmt.Errorf("rollback completed after healthcheck failure")
		}

		if opt.WaitAfterHealthy > 0 {
			u.log.Infof("==> Waiting for healthy containers to settle down (%d seconds)", opt.WaitAfterHealthy)
			time.Sleep(time.Duration(opt.WaitAfterHealthy) * time.Second)
		}
	} else {
		u.log.Infof("==> Waiting for new containers to be ready (%d seconds)", opt.NoHealthcheckTimeout)
		time.Sleep(time.Duration(opt.NoHealthcheckTimeout) * time.Second)
	}

	switch opt.ProxyType {
	case "traefik":
		u.log.Infof("==> Updating Traefik config for service: %s", opt.Service)
		if err := traefik.UpdateContainerIDsInConfig(opt.TraefikConfigFile, oldIDs, newIDs); err != nil {
			return err
		}
	}

	u.log.Infof("==> Sleeping %d second, after that, stopping and removing old containers", opt.NoHealthcheckTimeout)
	time.Sleep(time.Duration(opt.NoHealthcheckTimeout) * time.Second)

	guard.Disarm()
	u.log.Infof("==> These containers %v will be stopped and removed", oldIDs)
	if err := u.docker.Stop(ctx, oldIDs); err != nil {
		return err
	}
	if err := u.docker.Remove(ctx, oldIDs); err != nil {
		return err
	}

	return u.generator.Generate(ctx, opt.ComposeFiles, opt.EnvFiles, opt.TraefikConfigFile)
}

func (u *Updater) waitHealthy(ctx context.Context, containerIDs []string, expected int, timeoutSec int) (bool, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		okCount := 0
		for _, id := range containerIDs {
			status, err := u.docker.HealthStatus(ctx, id)
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

	okCount := 0
	for _, id := range containerIDs {
		status, err := u.docker.HealthStatus(ctx, id)
		if err != nil {
			return false, err
		}
		if status == "healthy" {
			okCount++
		}
	}
	return okCount == expected, nil
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

func validateProxyType(proxyType string) error {
	switch proxyType {
	case "traefik":
		return nil
	case "nginx-proxy":
		return fmt.Errorf("nginx-proxy support not implemented yet")
	default:
		return fmt.Errorf("unknown proxy type: %s", proxyType)
	}
}
