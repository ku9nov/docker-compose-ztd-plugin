package compose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/ku9nov/docker-compose-ztd-plugin/traefik"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// ComposeService represents a service in docker-compose file
type ComposeService struct {
	Image         string            `yaml:"image"`
	ContainerName string            `yaml:"container_name"`
	Environment   map[string]string `yaml:"environment"`
}

// ComposeFile represents the structure of a docker-compose file
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeClient handles Docker Compose operations
type ComposeClient struct {
	log          *logrus.Logger
	client       *client.Client
	composeFiles []string
	traefikConf  string
}

// NewComposeClient creates a new Compose client
func NewComposeClient(log *logrus.Logger, composeFiles []string) *ComposeClient {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	return &ComposeClient{
		log:          log,
		client:       cli,
		composeFiles: composeFiles,
		traefikConf:  "traefik/dynamic_conf.yml",
	}
}

// SetTraefikConf sets the Traefik configuration file path
func (c *ComposeClient) SetTraefikConf(path string) {
	c.traefikConf = path
}

// parseComposeFile reads and parses a docker-compose file
func (c *ComposeClient) parseComposeFile(filePath string) (*ComposeFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %v", err)
	}

	var composeFile ComposeFile
	if err := yaml.Unmarshal(data, &composeFile); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %v", err)
	}

	return &composeFile, nil
}

// streamContainerLogs streams logs from a container
func (c *ComposeClient) streamContainerLogs(ctx context.Context, containerID string) error {
	c.log.Infof("Streaming logs for container: %s", containerID)

	out, err := c.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to get container logs: %v", err)
	}
	defer out.Close()

	// Stream logs to stdout
	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, out)
	if err != nil {
		return fmt.Errorf("failed to stream container logs: %v", err)
	}
	return nil
}

// DeployService deploys a service using Docker API
func (c *ComposeClient) DeployService(ctx context.Context, serviceName string, detach bool, composeFiles []string, envFiles []string) error {
	c.log.Infof("Deploying service: %s (detached: %v)", serviceName, detach)

	// If serviceName is "up", use docker-compose command
	if serviceName == "up" {
		// Build docker-compose command
		args := []string{"compose"}

		// Add compose files
		for _, file := range composeFiles {
			args = append(args, "-f", file)
		}

		// Add env files
		for _, file := range envFiles {
			args = append(args, "--env-file", file)
		}

		// Always add up command with -d flag
		args = append(args, "up", "-d")

		// Create command
		cmd := exec.CommandContext(ctx, "docker", args...)

		// Set up pipes for output
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Run the command
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run docker-compose up: %v", err)
		}

		// After docker compose up is running, continue with our logic
		c.log.Info("Docker compose up is running, continuing with service deployment...")

		// Get project name from compose file
		projectName := ""
		if len(composeFiles) > 0 {
			// Get project name from compose file name
			baseName := filepath.Base(composeFiles[0])
			projectName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
		}

		// Get all containers to check their status
		containers, err := c.client.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			c.log.Errorf("Failed to list containers: %v", err)
			return nil
		}

		// Wait for containers to be running
		maxRetries := 30             // Maximum number of retries
		retryInterval := time.Second // Interval between retries

		for retry := 0; retry < maxRetries; retry++ {
			allRunning := true
			for _, container := range containers {
				// Skip containers that are not part of our compose project
				if container.Labels["com.docker.compose.project"] != projectName {
					continue
				}

				containerInfo, err := c.client.ContainerInspect(ctx, container.ID)
				if err != nil {
					c.log.Warnf("Failed to inspect container %s: %v", container.ID[:12], err)
					allRunning = false
					break
				}

				if !containerInfo.State.Running {
					c.log.Infof("Container %s is not running yet (State: %s)", container.ID[:12], containerInfo.State.Status)
					allRunning = false
					break
				}
			}

			if allRunning {
				c.log.Info("All containers are running")
				break
			}

			if retry < maxRetries-1 {
				c.log.Infof("Waiting for containers to be ready (attempt %d/%d)...", retry+1, maxRetries)
				time.Sleep(retryInterval)
			}
		}

		// Generate Traefik configuration
		c.log.Info("Generating Traefik configuration...")
		if err := c.generateTraefikConfig(ctx); err != nil {
			c.log.Errorf("Failed to generate Traefik configuration: %v", err)
		}

		// If not detached, show logs using docker compose logs command
		if !detach {
			c.log.Info("Showing container logs...")
			logsArgs := []string{"compose"}
			for _, file := range composeFiles {
				logsArgs = append(logsArgs, "-f", file)
			}

			logsArgs = append(logsArgs, "logs", "--follow", "--tail=1")

			logsCmd := exec.CommandContext(ctx, "docker", logsArgs...)
			logsCmd.Stdout = os.Stdout
			logsCmd.Stderr = os.Stderr

			if err := logsCmd.Run(); err != nil {
				c.log.Errorf("Failed to stream logs: %v", err)
			}
		}

		return nil
	}

	// For specific service deployment
	c.log.Infof("Deploying specific service: %s", serviceName)

	// Parse compose files
	var composeServices []string
	for _, file := range composeFiles {
		composeFile, err := c.parseComposeFile(file)
		if err != nil {
			return fmt.Errorf("failed to parse compose file %s: %v", file, err)
		}

		for serviceName := range composeFile.Services {
			composeServices = append(composeServices, serviceName)
		}
	}

	c.log.Infof("Found services in compose files: %v", composeServices)

	// Check if service exists in compose files
	serviceExists := false
	for _, s := range composeServices {
		if s == serviceName {
			serviceExists = true
			break
		}
	}

	if !serviceExists {
		return fmt.Errorf("service %s not found in compose files", serviceName)
	}

	// Get existing containers for the service
	existingContainers, err := c.GetServiceContainers(ctx, serviceName, composeFiles, envFiles)
	if err != nil {
		return fmt.Errorf("failed to get service containers: %v", err)
	}

	if len(existingContainers) > 0 {
		// Get the first container's info
		containerInfo, err := c.client.ContainerInspect(ctx, existingContainers[0])
		if err != nil {
			return fmt.Errorf("failed to inspect container: %v", err)
		}

		c.log.Infof("Found existing container: %s (State: %s)", existingContainers[0][:12], containerInfo.State.Status)

		// Container exists, start it if stopped
		if containerInfo.State.Status == "exited" || containerInfo.State.Status == "created" {
			c.log.Infof("Starting existing container: %s", existingContainers[0][:12])

			if err := c.client.ContainerStart(ctx, existingContainers[0], container.StartOptions{}); err != nil {
				return fmt.Errorf("failed to start container: %v", err)
			}

			// If not detached, stream logs
			if !detach {
				return c.streamContainerLogs(ctx, existingContainers[0])
			}
		} else {
			c.log.Infof("Container %s is already running", existingContainers[0][:12])
		}

		// Generate Traefik configuration
		if err := c.generateTraefikConfig(ctx); err != nil {
			c.log.Errorf("Failed to generate Traefik configuration: %v", err)
		}

		return nil
	}

	// Container doesn't exist, create and start it
	c.log.Infof("Creating new container for service: %s", serviceName)

	// Build docker-compose command
	args := []string{"compose"}

	// Add compose files
	for _, file := range composeFiles {
		args = append(args, "-f", file)
	}

	// Add env files
	for _, file := range envFiles {
		args = append(args, "--env-file", file)
	}

	// Add up command with -d flag and --no-recreate
	args = append(args, "up", "-d", "--no-recreate", serviceName)

	// Create command
	cmd := exec.CommandContext(ctx, "docker", args...)

	// Set up pipes for output
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run docker-compose up: %v", err)
	}

	// If not detached, stream logs
	if !detach {
		return c.streamContainerLogs(ctx, serviceName)
	}

	// Generate Traefik configuration
	if err := c.generateTraefikConfig(ctx); err != nil {
		c.log.Errorf("Failed to generate Traefik configuration: %v", err)
	}

	return nil
}

// generateTraefikConfig generates the Traefik configuration
func (c *ComposeClient) generateTraefikConfig(ctx context.Context) error {
	// Ensure Traefik directory exists
	if err := traefik.EnsureTraefikDir(c.log); err != nil {
		return fmt.Errorf("failed to ensure Traefik directory: %v", err)
	}

	// Get all containers
	containers, err := c.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %v", err)
	}

	// Get container IPs
	serviceIPs := make(map[string][]string)
	for _, container := range containers {
		serviceName := container.Labels["com.docker.compose.service"]
		if serviceName == "" {
			continue
		}

		// Get container network settings
		containerInfo, err := c.client.ContainerInspect(ctx, container.ID)
		if err != nil {
			c.log.Warnf("Failed to inspect container %s: %v", container.ID[:12], err)
			continue
		}

		// Get container IP address
		for _, network := range containerInfo.NetworkSettings.Networks {
			if network.IPAddress != "" {
				serviceIPs[serviceName] = append(serviceIPs[serviceName], network.IPAddress)
			}
		}
	}

	// Generate Traefik configuration
	return traefik.GenerateConfig(c.log, containers, serviceIPs, c.composeFiles, c.traefikConf)
}

// ScaleService scales a service to the specified number of replicas
func (c *ComposeClient) ScaleService(ctx context.Context, serviceName string, replicas int, composeFiles []string, envFiles []string) ([]string, error) {
	c.log.Infof("Scaling service %s to %d replicas", serviceName, replicas)

	// Build compose command
	args := []string{}
	for _, file := range composeFiles {
		args = append(args, "-f", file)
	}
	for _, file := range envFiles {
		args = append(args, "--env-file", file)
	}
	args = append(args, "up", "--detach", "--scale", fmt.Sprintf("%s=%d", serviceName, replicas), "--no-recreate", serviceName)

	// Execute compose command
	cmd := exec.CommandContext(ctx, "docker", "compose")
	cmd.Args = append(cmd.Args, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to scale service: %v, output: %s", err, string(output))
	}

	// Get new container IDs
	newContainers, err := c.GetServiceContainers(ctx, serviceName, composeFiles, envFiles)
	if err != nil {
		return nil, err
	}

	return newContainers, nil
}

// GetServiceContainers returns a list of container IDs for a given service
func (c *ComposeClient) GetServiceContainers(ctx context.Context, serviceName string, composeFiles []string, envFiles []string) ([]string, error) {
	containers, err := c.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %v", err)
	}

	var serviceContainers []string
	for _, container := range containers {
		// Check if the container belongs to the specified service using Docker Compose labels
		if container.Labels["com.docker.compose.service"] == serviceName {
			serviceContainers = append(serviceContainers, container.ID)
		}
	}

	c.log.Infof("Found %d containers for service %s", len(serviceContainers), serviceName)
	return serviceContainers, nil
}
