package traefik

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

type CanaryConfigInput struct {
	Service        string
	ProductionRule string
	Port           string
	OldIDs         []string
	NewIDs         []string
	NewWeight      int
	TCPRouters     []TCPRouteInput
	HealthCheck    *types.HealthChecks
}

func ApplyCanaryConfig(path string, input CanaryConfigInput) error {
	if strings.TrimSpace(input.Service) == "" {
		return fmt.Errorf("service is required")
	}
	if strings.TrimSpace(input.ProductionRule) == "" {
		return fmt.Errorf("production rule is required")
	}
	if input.NewWeight < 0 || input.NewWeight > 100 {
		return fmt.Errorf("new weight must be between 0 and 100")
	}
	if strings.TrimSpace(input.Port) == "" {
		input.Port = "80"
	}

	oldWeight := 100 - input.NewWeight
	if oldWeight > 0 && len(input.OldIDs) == 0 {
		return fmt.Errorf("old containers are required when old weight is %d", oldWeight)
	}
	if input.NewWeight > 0 && len(input.NewIDs) == 0 {
		return fmt.Errorf("new containers are required when new weight is %d", input.NewWeight)
	}

	cfg, err := readDynamicConfig(path)
	if err != nil {
		return err
	}
	ensureHTTPConfig(&cfg)
	ensureTCPConfig(&cfg)

	oldService := canaryServiceName(input.Service, "old")
	newService := canaryServiceName(input.Service, "new")

	setOrDeleteHTTPService(cfg.HTTP.Services, oldService, input.OldIDs, input.Port, input.HealthCheck)
	setOrDeleteHTTPService(cfg.HTTP.Services, newService, input.NewIDs, input.Port, input.HealthCheck)

	weighted := make([]types.HTTPWeightedService, 0, 2)
	if oldWeight > 0 {
		weighted = append(weighted, types.HTTPWeightedService{
			Name:   oldService,
			Weight: oldWeight,
		})
	}
	if input.NewWeight > 0 {
		weighted = append(weighted, types.HTTPWeightedService{
			Name:   newService,
			Weight: input.NewWeight,
		})
	}
	if len(weighted) == 0 {
		return fmt.Errorf("both old and new weights are zero")
	}

	setOrDeleteWeightedHTTPService(cfg.HTTP.Services, input.Service, weighted)
	cfg.HTTP.Routers[input.Service] = types.HTTPRouter{
		Rule:    input.ProductionRule,
		Service: input.Service,
	}

	for _, tcp := range input.TCPRouters {
		baseName := strings.TrimSpace(tcp.BackendBaseName)
		if baseName == "" {
			baseName = tcp.RouterService
		}

		oldTCPService := canaryServiceName(baseName, "old")
		newTCPService := canaryServiceName(baseName, "new")
		setOrDeleteTCPService(cfg.TCP.Services, oldTCPService, input.OldIDs, tcp.BackendPort)
		setOrDeleteTCPService(cfg.TCP.Services, newTCPService, input.NewIDs, tcp.BackendPort)

		tcpWeighted := make([]types.TCPWeightedService, 0, 2)
		if oldWeight > 0 {
			tcpWeighted = append(tcpWeighted, types.TCPWeightedService{
				Name:   oldTCPService,
				Weight: oldWeight,
			})
		}
		if input.NewWeight > 0 {
			tcpWeighted = append(tcpWeighted, types.TCPWeightedService{
				Name:   newTCPService,
				Weight: input.NewWeight,
			})
		}
		setOrDeleteWeightedTCPService(cfg.TCP.Services, tcp.RouterService, tcpWeighted)
		cfg.TCP.Routers[tcp.RouterName] = types.TCPRouter{
			Rule:        tcp.Rule,
			Service:     tcp.RouterService,
			EntryPoints: append([]string{}, tcp.EntryPoints...),
		}
	}

	pruneEmptyDynamicConfigSections(&cfg)

	data, err := configio.MarshalYAML(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return configio.WriteAtomic(path, data, 0o644)
}

func canaryServiceName(service string, side string) string {
	return service + "_" + side
}
