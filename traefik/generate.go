package traefik

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// TraefikConfig represents the structure of the Traefik dynamic configuration
type TraefikConfig struct {
	HTTP struct {
		Routers  map[string]Router  `yaml:"routers"`
		Services map[string]Service `yaml:"services"`
	} `yaml:"http"`
}

// Router represents a Traefik router configuration
type Router struct {
	Rule    string `yaml:"rule"`
	Service string `yaml:"service"`
}

// Service represents a Traefik service configuration
type Service struct {
	LoadBalancer LoadBalancer `yaml:"loadbalancer"`
}

// LoadBalancer represents a Traefik load balancer configuration
type LoadBalancer struct {
	Servers     []Server     `yaml:"servers"`
	HealthCheck *HealthCheck `yaml:"healthcheck,omitempty"`
}

// Server represents a Traefik server configuration
type Server struct {
	URL string `yaml:"url"`
}

// HealthCheck represents a Traefik health check configuration
type HealthCheck struct {
	Path            string            `yaml:"path,omitempty"`
	Interval        string            `yaml:"interval,omitempty"`
	Timeout         string            `yaml:"timeout,omitempty"`
	Scheme          string            `yaml:"scheme,omitempty"`
	Mode            string            `yaml:"mode,omitempty"`
	Hostname        string            `yaml:"hostname,omitempty"`
	Port            string            `yaml:"port,omitempty"`
	FollowRedirects string            `yaml:"followRedirects,omitempty"`
	Method          string            `yaml:"method,omitempty"`
	Status          string            `yaml:"status,omitempty"`
	Headers         map[string]string `yaml:"headers,omitempty"`
}

// EnsureTraefikDir ensures that the Traefik directory exists with correct permissions
func EnsureTraefikDir(log *logrus.Logger) error {
	traefikDir := "traefik"

	// Check if directory exists
	if _, err := os.Stat(traefikDir); os.IsNotExist(err) {
		log.Infof("Creating Traefik directory: %s", traefikDir)

		// Create directory
		if err := os.MkdirAll(traefikDir, 0755); err != nil {
			return fmt.Errorf("failed to create Traefik directory: %v", err)
		}

		// Get current user
		currentUser, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get current user: %v", err)
		}

		// Convert username to uid
		uid, err := strconv.Atoi(currentUser.Uid)
		if err != nil {
			return fmt.Errorf("failed to convert uid: %v", err)
		}

		// Convert group to gid
		gid, err := strconv.Atoi(currentUser.Gid)
		if err != nil {
			return fmt.Errorf("failed to convert gid: %v", err)
		}

		// Change ownership
		if err := os.Chown(traefikDir, uid, gid); err != nil {
			return fmt.Errorf("failed to change directory ownership: %v", err)
		}
	}

	return nil
}

// GenerateConfig generates the Traefik dynamic configuration
func GenerateConfig(log *logrus.Logger, containers []container.Summary, serviceIPs map[string][]string, composeFiles []string, configPath string) error {
	log.Info("Starting Traefik configuration generation...")

	// Get services from compose files
	composeServices := make(map[string]bool)
	for _, file := range composeFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read compose file %s: %v", file, err)
		}

		var composeFile struct {
			Services map[string]interface{} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &composeFile); err != nil {
			return fmt.Errorf("failed to parse compose file %s: %v", file, err)
		}

		for serviceName := range composeFile.Services {
			composeServices[serviceName] = true
		}
	}

	config := &TraefikConfig{
		HTTP: struct {
			Routers  map[string]Router  `yaml:"routers"`
			Services map[string]Service `yaml:"services"`
		}{
			Routers:  make(map[string]Router),
			Services: make(map[string]Service),
		},
	}

	// Create a map of service names to their containers
	serviceContainers := make(map[string][]container.Summary)
	for _, container := range containers {
		serviceName := container.Labels["com.docker.compose.service"]
		if serviceName == "" {
			continue
		}
		// Only include containers for services defined in compose files
		if composeServices[serviceName] {
			serviceContainers[serviceName] = append(serviceContainers[serviceName], container)
		}
	}

	// Process each service
	for serviceName, serviceContainers := range serviceContainers {
		// Skip if no containers for this service
		if len(serviceContainers) == 0 {
			continue
		}

		// Use the first container to get service configuration
		container := serviceContainers[0]
		log.Infof("Processing service: %s", serviceName)

		// Check if service has Traefik enabled
		if container.Labels["traefik.enable"] != "true" {
			log.Infof("Skipping service %s (Traefik not enabled)", serviceName)
			continue
		}

		// Get router rule
		ruleKey := fmt.Sprintf("traefik.http.routers.%s.rule", serviceName)
		rule := container.Labels[ruleKey]
		if rule == "" {
			log.Warnf("No router rule found for service %s", serviceName)
			continue
		}

		log.Infof("Found router rule for %s: %s", serviceName, rule)

		// Get server port
		portKey := fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", serviceName)
		port := container.Labels[portKey]
		if port == "" {
			port = "80"
		}

		// Create servers list
		var servers []Server
		for _, c := range serviceContainers {
			serverURL := fmt.Sprintf("http://%s:%s", c.ID[:12], port)
			servers = append(servers, Server{URL: serverURL})
			log.Infof("Added server for %s: %s", serviceName, serverURL)
		}

		if len(servers) == 0 {
			log.Warnf("No servers found for service %s", serviceName)
			continue
		}

		// Create service
		service := Service{
			LoadBalancer: LoadBalancer{
				Servers: servers,
			},
		}

		// Add health check if configured
		healthCheck := extractHealthCheck(container.Labels, serviceName)
		if healthCheck != nil {
			// Only add healthcheck if it has at least one non-empty field
			hasValues := healthCheck.Path != "" ||
				healthCheck.Interval != "" ||
				healthCheck.Timeout != "" ||
				healthCheck.Scheme != "" ||
				healthCheck.Mode != "" ||
				healthCheck.Hostname != "" ||
				healthCheck.Port != "" ||
				healthCheck.FollowRedirects != "" ||
				healthCheck.Method != "" ||
				healthCheck.Status != "" ||
				len(healthCheck.Headers) > 0

			if hasValues {
				service.LoadBalancer.HealthCheck = healthCheck
				log.Infof("Added health check for service %s", serviceName)
			}
		}

		// Add router and service to config
		config.HTTP.Routers[serviceName] = Router{
			Rule:    rule,
			Service: serviceName,
		}
		config.HTTP.Services[serviceName] = service

		log.Infof("Added configuration for service %s", serviceName)
	}

	// Convert to YAML
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %v", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	log.Infof("Traefik configuration written to %s", configPath)
	log.Info("Traefik configuration generation completed successfully")
	return nil
}

// extractHealthCheck extracts health check configuration from container labels
func extractHealthCheck(labels map[string]string, serviceName string) *HealthCheck {
	prefix := fmt.Sprintf("traefik.http.services.%s.loadbalancer.healthCheck", serviceName)

	// Check if any health check configuration exists
	hasConfig := false
	for key := range labels {
		if strings.HasPrefix(key, prefix) {
			hasConfig = true
			break
		}
	}

	if !hasConfig {
		return nil
	}

	healthCheck := &HealthCheck{}

	// Only set fields that have non-empty values
	if path := labels[prefix+".path"]; path != "" {
		healthCheck.Path = path
	}
	if interval := labels[prefix+".interval"]; interval != "" {
		healthCheck.Interval = interval
	}
	if timeout := labels[prefix+".timeout"]; timeout != "" {
		healthCheck.Timeout = timeout
	}
	if scheme := labels[prefix+".scheme"]; scheme != "" {
		healthCheck.Scheme = scheme
	}
	if mode := labels[prefix+".mode"]; mode != "" {
		healthCheck.Mode = mode
	}
	if hostname := labels[prefix+".hostname"]; hostname != "" {
		healthCheck.Hostname = hostname
	}
	if port := labels[prefix+".port"]; port != "" {
		healthCheck.Port = port
	}
	if followRedirects := labels[prefix+".followRedirects"]; followRedirects != "" {
		healthCheck.FollowRedirects = followRedirects
	}
	if method := labels[prefix+".method"]; method != "" {
		healthCheck.Method = method
	}
	if status := labels[prefix+".status"]; status != "" {
		healthCheck.Status = status
	}

	// Extract headers
	headers := make(map[string]string)
	for key, value := range labels {
		if key == prefix+".headers" {
			// Parse JSON headers if present
			var parsedHeaders map[string]string
			if err := json.Unmarshal([]byte(value), &parsedHeaders); err == nil {
				headers = parsedHeaders
			}
		} else if strings.HasPrefix(key, prefix+".headers.") {
			// Handle individual header entries
			headerName := strings.TrimPrefix(key, prefix+".headers.")
			headers[headerName] = value
		}
	}

	if len(headers) > 0 {
		healthCheck.Headers = headers
	}

	return healthCheck
}

// UpdateConfig updates container IDs in the Traefik configuration
func UpdateConfig(log *logrus.Logger, oldContainers, newContainers []string, configPath string) error {
	log.Info("Updating Traefik configuration with new container IDs...")

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	var config TraefikConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	// Create a map of old to new container IDs
	containerMap := make(map[string]string)
	for i, oldID := range oldContainers {
		if i < len(newContainers) {
			containerMap[oldID[:12]] = newContainers[i][:12]
		}
	}

	// Update container IDs in services
	for serviceName, service := range config.HTTP.Services {
		for i, server := range service.LoadBalancer.Servers {
			// Extract container ID from URL
			parts := strings.Split(server.URL, "//")
			if len(parts) != 2 {
				continue
			}
			hostPort := strings.Split(parts[1], ":")
			if len(hostPort) != 2 {
				continue
			}
			oldContainerID := hostPort[0]

			// Check if we need to update this container ID
			if newID, exists := containerMap[oldContainerID]; exists {
				// Update the URL with new container ID
				config.HTTP.Services[serviceName].LoadBalancer.Servers[i].URL = fmt.Sprintf("http://%s:%s", newID, hostPort[1])
				log.Infof("Updated container ID for service %s: %s -> %s", serviceName, oldContainerID, newID)
			}
		}
	}

	// Write updated config
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %v", err)
	}

	if err := os.WriteFile(configPath, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write updated config: %v", err)
	}

	log.Info("Traefik configuration updated successfully")
	return nil
}
