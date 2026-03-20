package traefik

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

const qaRouterPriority = 100

type BlueGreenConfigInput struct {
	Service        string
	Active         string
	ProductionRule string
	Port           string
	BlueIDs        []string
	GreenIDs       []string
	QA             *state.QAModes
	HealthCheck    *types.HealthChecks
}

func ApplyBlueGreenConfig(path string, input BlueGreenConfigInput) error {
	if strings.TrimSpace(input.Service) == "" {
		return fmt.Errorf("service is required")
	}
	if input.Active != state.ColorBlue && input.Active != state.ColorGreen {
		return fmt.Errorf("active color must be blue or green")
	}
	if strings.TrimSpace(input.ProductionRule) == "" {
		return fmt.Errorf("production rule is required")
	}
	if strings.TrimSpace(input.Port) == "" {
		input.Port = "80"
	}

	cfg, err := readDynamicConfig(path)
	if err != nil {
		return err
	}
	ensureHTTPConfig(&cfg)

	blueService := serviceColorName(input.Service, state.ColorBlue)
	greenService := serviceColorName(input.Service, state.ColorGreen)

	setOrDeleteHTTPService(cfg.HTTP.Services, blueService, input.BlueIDs, input.Port, input.HealthCheck)
	setOrDeleteHTTPService(cfg.HTTP.Services, greenService, input.GreenIDs, input.Port, input.HealthCheck)
	delete(cfg.HTTP.Services, input.Service)

	activeService := blueService
	if input.Active == state.ColorGreen {
		activeService = greenService
	}
	cfg.HTTP.Routers[input.Service] = types.HTTPRouter{
		Rule:    input.ProductionRule,
		Service: activeService,
	}

	greenRuleSource := input.ProductionRule
	setOrDeleteQARouter(cfg.HTTP.Routers, qaRouterName(input.Service, "host"), hostModeRule(input.QA), greenService)
	setOrDeleteQARouter(cfg.HTTP.Routers, qaRouterName(input.Service, "headers"), appendRule(greenRuleSource, headerModeExpr(input.QA)), greenService)
	setOrDeleteQARouter(cfg.HTTP.Routers, qaRouterName(input.Service, "cookies"), appendRule(greenRuleSource, cookieModeExpr(input.QA)), greenService)
	setOrDeleteQARouter(cfg.HTTP.Routers, qaRouterName(input.Service, "ip"), appendRule(greenRuleSource, ipModeExpr(input.QA)), greenService)

	data, err := configio.MarshalYAML(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return configio.WriteAtomic(path, data, 0o644)
}

func readDynamicConfig(path string) (types.DynamicConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return types.DynamicConfig{}, nil
		}
		return types.DynamicConfig{}, err
	}
	var cfg types.DynamicConfig
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}
	if err := configio.UnmarshalYAML(data, &cfg); err != nil {
		return types.DynamicConfig{}, err
	}
	return cfg, nil
}

func ensureHTTPConfig(cfg *types.DynamicConfig) {
	if cfg.HTTP == nil {
		cfg.HTTP = &types.HTTPConfig{}
	}
	if cfg.HTTP.Routers == nil {
		cfg.HTTP.Routers = map[string]types.HTTPRouter{}
	}
	if cfg.HTTP.Services == nil {
		cfg.HTTP.Services = map[string]types.HTTPService{}
	}
}

func setOrDeleteHTTPService(services map[string]types.HTTPService, name string, ids []string, port string, hc *types.HealthChecks) {
	if len(ids) == 0 {
		delete(services, name)
		return
	}
	servers := make([]types.HTTPServer, 0, len(ids))
	for _, id := range ids {
		servers = append(servers, types.HTTPServer{
			URL: "http://" + shortID(id) + ":" + port,
		})
	}
	svc := types.HTTPService{
		LoadBalancer: &types.HTTPLoadBalancer{
			Servers: servers,
		},
	}
	if hc != nil {
		svc.LoadBalancer.HealthCheck = hc
	}
	services[name] = svc
}

func setOrDeleteWeightedHTTPService(services map[string]types.HTTPService, name string, weighted []types.HTTPWeightedService) {
	if len(weighted) == 0 {
		delete(services, name)
		return
	}
	services[name] = types.HTTPService{
		Weighted: &types.HTTPWeightedRoute{
			Services: weighted,
		},
	}
}

func setOrDeleteQARouter(routers map[string]types.HTTPRouter, name string, rule string, service string) {
	if strings.TrimSpace(rule) == "" {
		delete(routers, name)
		return
	}
	routers[name] = types.HTTPRouter{
		Rule:     rule,
		Service:  service,
		Priority: qaRouterPriority,
	}
}

func serviceColorName(service string, color string) string {
	return service + "-" + color
}

func qaRouterName(service string, mode string) string {
	return service + "-qa-" + mode
}

func hostModeRule(qa *state.QAModes) string {
	if qa == nil || strings.TrimSpace(qa.Host) == "" {
		return ""
	}
	value := strings.TrimSpace(qa.Host)
	if strings.Contains(value, "Host(") {
		return value
	}
	return fmt.Sprintf("Host(`%s`)", value)
}

func headerModeExpr(qa *state.QAModes) string {
	if qa == nil || strings.TrimSpace(qa.Headers) == "" {
		return ""
	}
	parts := strings.SplitN(strings.TrimSpace(qa.Headers), "=", 2)
	if len(parts) != 2 {
		return ""
	}
	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if name == "" || value == "" {
		return ""
	}
	return fmt.Sprintf("Header(`%s`, `%s`)", name, value)
}

func cookieModeExpr(qa *state.QAModes) string {
	if qa == nil || strings.TrimSpace(qa.Cookies) == "" {
		return ""
	}
	parts := strings.SplitN(strings.TrimSpace(qa.Cookies), "=", 2)
	if len(parts) != 2 {
		return ""
	}
	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if name == "" || value == "" {
		return ""
	}
	needle := regexp.QuoteMeta(name + "=" + value)
	return fmt.Sprintf("HeaderRegexp(`Cookie`, `(^|;\\s*)%s(;|$)`)", needle)
}

func ipModeExpr(qa *state.QAModes) string {
	if qa == nil || strings.TrimSpace(qa.IP) == "" {
		return ""
	}
	value := strings.TrimSpace(qa.IP)
	if strings.Contains(value, "ClientIP(") {
		return value
	}
	return fmt.Sprintf("ClientIP(`%s`)", value)
}

func appendRule(base string, expr string) string {
	base = strings.TrimSpace(base)
	expr = strings.TrimSpace(expr)
	if base == "" {
		return expr
	}
	if expr == "" {
		return ""
	}
	return base + " && " + expr
}
