package bluegreen

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
)

var errBlueGreenStateNotFound = errors.New("blue-green state not found")

func (d *Deployer) switchTraffic(ctx context.Context, opt Options) error {
	project, currentState, err := d.findStateByService(opt.Service)
	if err != nil {
		return err
	}

	targetColor := opt.SwitchTo
	if targetColor == "" {
		targetColor = state.ColorGreen
		if currentState.Active == state.ColorGreen {
			targetColor = state.ColorBlue
		}
	}
	if targetColor != state.ColorBlue && targetColor != state.ColorGreen {
		return fmt.Errorf("invalid switch target color: %s", targetColor)
	}
	if targetColor == state.ColorBlue && len(currentState.Blue) == 0 {
		return fmt.Errorf("cannot switch to blue: no blue containers in state")
	}
	if targetColor == state.ColorGreen && len(currentState.Green) == 0 {
		return fmt.Errorf("cannot switch to green: no green containers in state")
	}
	d.log.Infof("==> Switching service '%s' traffic: %s -> %s", currentState.Service, currentState.Active, targetColor)

	labels, err := d.labelsFromState(ctx, currentState)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, currentState.Service)
	hc := extractHealthCheck(labels, currentState.Service)

	if err := traefik.ApplyBlueGreenConfig(opt.TraefikConfigFile, traefik.BlueGreenConfigInput{
		Service:        currentState.Service,
		Active:         targetColor,
		ProductionRule: productionRule,
		Port:           port,
		BlueIDs:        currentState.Blue,
		GreenIDs:       currentState.Green,
		QA:             currentState.QA,
		HealthCheck:    hc,
	}); err != nil {
		return err
	}

	now := time.Now().UTC()
	currentState.Active = targetColor
	currentState.SwitchedAt = &now
	if opt.AutoCleanup > 0 {
		cleanupAt := now.Add(opt.AutoCleanup)
		currentState.CleanupAt = &cleanupAt
		var toCleanup []string
		toCleanupColor := state.ColorGreen
		if targetColor == state.ColorGreen {
			toCleanupColor = state.ColorBlue
			toCleanup = append(toCleanup, currentState.Blue...)
		} else {
			toCleanup = append(toCleanup, currentState.Green...)
		}
		d.log.Infof("==> Auto-cleanup scheduled at %s for %s containers: %v", cleanupAt.Format(time.RFC3339), toCleanupColor, toCleanup)
	}

	if err := d.store.Save(project, currentState); err != nil {
		return err
	}
	d.runMetricsGate(ctx, opt, blueGreenMetricServiceName(currentState.Service, targetColor), "switch")

	d.log.Infof("==> Switched service '%s' traffic to %s", currentState.Service, targetColor)
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
		if st.Service != service || st.Strategy != state.StrategyBlueGreen {
			continue
		}
		if matchedProject != "" {
			return "", state.DeploymentState{}, fmt.Errorf("multiple blue-green states found for service %s", service)
		}
		matchedProject = project
		matchedState = st
	}
	if matchedProject == "" {
		return "", state.DeploymentState{}, fmt.Errorf("%w for service %s", errBlueGreenStateNotFound, service)
	}
	return matchedProject, matchedState, nil
}

func (d *Deployer) labelsFromState(ctx context.Context, st state.DeploymentState) (map[string]string, error) {
	candidates := append([]string{}, st.Blue...)
	candidates = append(candidates, st.Green...)
	for _, id := range candidates {
		labels, err := d.docker.Labels(ctx, id)
		if err == nil && len(labels) > 0 {
			return labels, nil
		}
	}

	return nil, fmt.Errorf("unable to read labels from blue/green containers")
}
