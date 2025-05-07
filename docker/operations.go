package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/ku9nov/docker-compose-ztd-plugin/compose"
	"github.com/sirupsen/logrus"
)

// DockerClient wraps the Docker client and provides methods for Docker operations
type DockerClient struct {
	client  *client.Client
	compose *compose.ComposeClient
	log     *logrus.Logger
}

// NewDockerClient creates a new Docker client
func NewDockerClient(log *logrus.Logger) (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %v", err)
	}

	composeClient := compose.NewComposeClient(log, []string{})
	return &DockerClient{
		log:     log,
		client:  cli,
		compose: composeClient,
	}, nil
}

// SetComposeFiles sets the compose files for the Docker client
func (d *DockerClient) SetComposeFiles(composeFiles []string) {
	d.compose = compose.NewComposeClient(d.log, composeFiles)
}

// SetTraefikConf sets the Traefik configuration file path
func (d *DockerClient) SetTraefikConf(path string) {
	d.compose.SetTraefikConf(path)
}

// DeployService deploys a service using Docker Compose
func (d *DockerClient) DeployService(ctx context.Context, serviceName string, detach bool, composeFiles []string, envFiles []string) error {
	return d.compose.DeployService(ctx, serviceName, detach, composeFiles, envFiles)
}

// ScaleService scales a service to the specified number of replicas
func (d *DockerClient) ScaleService(ctx context.Context, serviceName string, replicas int, composeFiles []string, envFiles []string) ([]string, error) {
	return d.compose.ScaleService(ctx, serviceName, replicas, composeFiles, envFiles)
}

// GetServiceContainers returns a list of container IDs for a given service
func (d *DockerClient) GetServiceContainers(ctx context.Context, serviceName string, composeFiles []string, envFiles []string) ([]string, error) {
	return d.compose.GetServiceContainers(ctx, serviceName, composeFiles, envFiles)
}

// CheckContainerHealth checks if a container is healthy
func (d *DockerClient) CheckContainerHealth(ctx context.Context, containerID string) (bool, error) {
	container, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, fmt.Errorf("failed to inspect container: %v", err)
	}

	// If no health check is defined, consider the container healthy
	if container.State.Health == nil {
		return true, nil
	}

	return container.State.Health.Status == "healthy", nil
}

// WaitForHealthyContainers waits for containers to become healthy
func (d *DockerClient) WaitForHealthyContainers(ctx context.Context, containerIDs []string, timeout time.Duration) error {
	d.log.Infof("Waiting %s for containers to become healthy: %v", timeout, containerIDs)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allHealthy := true
		for _, containerID := range containerIDs {
			healthy, err := d.CheckContainerHealth(ctx, containerID)
			if err != nil {
				return fmt.Errorf("failed to check container health: %v", err)
			}
			d.log.Infof("Container %s is healthy: %t", containerID, healthy)
			if !healthy {
				allHealthy = false
				break
			}
		}
		if allHealthy {
			d.log.Infof("All containers are healthy")
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout waiting for containers to become healthy")
}

// StopAndRemoveContainers stops and removes containers
func (d *DockerClient) StopAndRemoveContainers(ctx context.Context, containerIDs []string) error {
	timeoutSeconds := 10
	for _, containerID := range containerIDs {
		d.log.Infof("Stopping container: %s", containerID)
		if err := d.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeoutSeconds}); err != nil {
			return fmt.Errorf("failed to stop container: %v", err)
		}

		d.log.Infof("Removing container: %s", containerID)
		if err := d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("failed to remove container: %v", err)
		}
	}
	return nil
}
