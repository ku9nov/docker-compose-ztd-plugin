package bluegreen

import (
	"context"
	"fmt"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/traefik"
)

func (d *Deployer) cleanupInactive(ctx context.Context, opt Options) error {
	project, st, err := d.findStateByService(opt.Service)
	if err != nil {
		return err
	}
	return d.CleanupProjectState(ctx, project, st, opt.TraefikConfigFile)
}

func (d *Deployer) CleanupProjectState(ctx context.Context, project string, st state.DeploymentState, traefikConfigFile string) error {
	var inactive []string
	inactiveColor := state.ColorGreen
	if st.Active == state.ColorBlue {
		inactive = append(inactive, st.Green...)
	} else {
		inactive = append(inactive, st.Blue...)
		inactiveColor = state.ColorBlue
	}

	d.log.Infof("==> Cleanup requested for service '%s': active=%s, removing inactive=%s", st.Service, st.Active, inactiveColor)
	if len(inactive) > 0 {
		d.log.Infof("==> Stopping inactive %s containers for service '%s': %v", inactiveColor, st.Service, inactive)
		if err := d.docker.Stop(ctx, inactive); err != nil {
			return err
		}
		d.log.Infof("==> Removing inactive %s containers for service '%s': %v", inactiveColor, st.Service, inactive)
		if err := d.docker.Remove(ctx, inactive); err != nil {
			return err
		}
	} else {
		d.log.Infof("==> No inactive %s containers found for service '%s'", inactiveColor, st.Service)
	}

	labels, err := d.labelsFromState(ctx, st)
	if err != nil {
		return err
	}
	productionRule, port := productionRuleAndPort(labels, st.Service)
	hc := extractHealthCheck(labels, st.Service)

	activeBlue := st.Blue
	activeGreen := st.Green
	if st.Active == state.ColorBlue {
		activeGreen = nil
	} else {
		activeBlue = nil
	}

	if err := traefik.ApplyBlueGreenConfig(traefikConfigFile, traefik.BlueGreenConfigInput{
		Service:        st.Service,
		Active:         st.Active,
		ProductionRule: productionRule,
		Port:           port,
		BlueIDs:        activeBlue,
		GreenIDs:       activeGreen,
		QA:             nil,
		HealthCheck:    hc,
	}); err != nil {
		return fmt.Errorf("failed to update traefik config after cleanup: %w", err)
	}

	if err := d.store.Delete(project); err != nil {
		return err
	}

	d.log.Infof("==> Blue-green state file removed for key '%s'", project)
	d.log.Infof("==> Cleanup completed for service '%s'", st.Service)
	return nil
}
