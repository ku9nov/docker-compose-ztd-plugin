package traefik

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

type Generator struct {
	compose compose.Adapter
	docker  labelReader
}

type labelReader interface {
	Labels(ctx context.Context, containerID string) (map[string]string, error)
}

func NewGenerator(composeAdapter compose.Adapter, dockerClient labelReader) *Generator {
	return &Generator{
		compose: composeAdapter,
		docker:  dockerClient,
	}
}

func (g *Generator) Generate(ctx context.Context, composeFiles []string, envFiles []string, outputPath string) error {
	enabledServices, err := collectTraefikEnabledServices(composeFiles)
	if err != nil {
		return err
	}
	if len(enabledServices) == 0 {
		return fmt.Errorf("no services with label traefik.enable=true were found")
	}

	serviceEndpoints := map[string][]string{}
	for _, svc := range enabledServices {
		ids, err := g.compose.PsQuiet(ctx, composeFiles, envFiles, svc)
		if err != nil {
			return err
		}
		for _, id := range ids {
			serviceEndpoints[svc] = append(serviceEndpoints[svc], shortID(id))
		}
	}

	allContainerIDs, err := g.compose.PsQuiet(ctx, composeFiles, envFiles, "")
	if err != nil {
		return err
	}

	cfg := types.DynamicConfig{
		HTTP: &types.HTTPConfig{
			Routers:  map[string]types.HTTPRouter{},
			Services: map[string]types.HTTPService{},
		},
		TCP: &types.TCPConfig{
			Routers:  map[string]types.TCPRouter{},
			Services: map[string]types.TCPService{},
		},
	}
	processedServices := map[string]struct{}{}

	for _, id := range allContainerIDs {
		labels, err := g.docker.Labels(ctx, id)
		if err != nil {
			return err
		}

		serviceName := labels["com.docker.compose.service"]
		if serviceName == "" {
			continue
		}

		endpoints, ok := serviceEndpoints[serviceName]
		if !ok || len(endpoints) == 0 {
			continue
		}
		if _, seen := processedServices[serviceName]; seen {
			continue
		}
		processedServices[serviceName] = struct{}{}

		routerRule := labels["traefik.http.routers."+serviceName+".rule"]
		if routerRule != "" {
			cfg.HTTP.Routers[serviceName] = types.HTTPRouter{
				Rule:    routerRule,
				Service: serviceName,
			}
		}

		httpPort := labels["traefik.http.services."+serviceName+".loadbalancer.server.port"]
		if httpPort == "" {
			httpPort = "80"
		}

		httpServers := make([]types.HTTPServer, 0, len(endpoints))
		for _, endpoint := range endpoints {
			httpServers = append(httpServers, types.HTTPServer{
				URL: "http://" + endpoint + ":" + httpPort,
			})
		}

		httpService := types.HTTPService{
			LoadBalancer: &types.HTTPLoadBalancer{
				Servers: httpServers,
			},
		}

		if hc := extractHealthCheck(labels, serviceName); hc != nil {
			httpService.LoadBalancer.HealthCheck = hc
		}
		cfg.HTTP.Services[serviceName] = httpService

		for _, tcp := range collectTCPRouterMeta(labels) {
			cfg.TCP.Routers[tcp.RouterName] = newTCPRouter(tcp.Rule, tcp.RouterService, tcp.EntryPoints, tcp.TLSEnabled)

			tcpServers := make([]types.TCPServer, 0, len(endpoints))
			for _, endpoint := range endpoints {
				tcpServers = append(tcpServers, types.TCPServer{
					Address: endpoint + ":" + tcp.BackendPort,
				})
			}
			cfg.TCP.Services[tcp.RouterService] = types.TCPService{
				LoadBalancer: &types.TCPLoadBalancer{
					Servers: tcpServers,
				},
			}
		}
	}

	if len(cfg.HTTP.Routers) == 0 && len(cfg.HTTP.Services) == 0 && len(cfg.TCP.Routers) == 0 && len(cfg.TCP.Services) == 0 {
		return fmt.Errorf("generated Traefik configuration is empty")
	}
	if len(cfg.HTTP.Routers) == 0 && len(cfg.HTTP.Services) == 0 {
		cfg.HTTP = nil
	}
	if len(cfg.TCP.Routers) == 0 && len(cfg.TCP.Services) == 0 {
		cfg.TCP = nil
	}

	data, err := configio.MarshalYAML(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return configio.WriteAtomic(outputPath, data, 0o644)
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

func pruneEmptyDynamicConfigSections(cfg *types.DynamicConfig) {
	if cfg.HTTP != nil && len(cfg.HTTP.Routers) == 0 && len(cfg.HTTP.Services) == 0 {
		cfg.HTTP = nil
	}
	if cfg.TCP != nil && len(cfg.TCP.Routers) == 0 && len(cfg.TCP.Services) == 0 {
		cfg.TCP = nil
	}
}
