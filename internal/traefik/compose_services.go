package traefik

import (
	"os"
	"sort"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Labels any `yaml:"labels"`
}

func collectTraefikEnabledServices(files []string) ([]string, error) {
	enabled := map[string]struct{}{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}

		var cfg composeFile
		if err := configio.UnmarshalYAML(data, &cfg); err != nil {
			return nil, err
		}
		for name, svc := range cfg.Services {
			if hasTraefikEnableLabel(svc.Labels) {
				enabled[name] = struct{}{}
			}
		}
	}

	services := make([]string, 0, len(enabled))
	for name := range enabled {
		services = append(services, name)
	}
	sort.Strings(services)
	return services, nil
}

func hasTraefikEnableLabel(labels any) bool {
	switch v := labels.(type) {
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if s == "traefik.enable=true" {
				return true
			}
		}
	case map[string]any:
		if val, ok := v["traefik.enable"]; ok {
			switch b := val.(type) {
			case bool:
				return b
			case string:
				return b == "true"
			}
		}
	}
	return false
}
