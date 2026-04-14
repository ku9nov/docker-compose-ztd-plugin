package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Client struct {
	dockerArgs []string
}

func NewClient(dockerArgs []string) *Client {
	return &Client{dockerArgs: append([]string{}, dockerArgs...)}
}

func (c *Client) HealthStatus(ctx context.Context, containerID string) (string, error) {
	out, err := c.inspect(ctx, "{{json .State.Health.Status}}", containerID)
	if err != nil {
		return "", err
	}

	status := strings.TrimSpace(out)
	status = strings.Trim(status, `"`)
	return status, nil
}

func (c *Client) HasHealthcheck(ctx context.Context, containerID string) (bool, error) {
	out, err := c.inspect(ctx, "{{json .State.Health}}", containerID)
	if err != nil {
		return false, err
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return false, nil
	}
	_, ok := raw["Status"]
	return ok, nil
}

func (c *Client) Labels(ctx context.Context, containerID string) (map[string]string, error) {
	out, err := c.inspect(ctx, "{{json .Config.Labels}}", containerID)
	if err != nil {
		return nil, err
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

func (c *Client) Stop(ctx context.Context, containerIDs []string) error {
	if len(containerIDs) == 0 {
		return nil
	}
	args := append([]string{}, c.dockerArgs...)
	args = append(args, "stop")
	args = append(args, containerIDs...)
	return c.run(ctx, args...)
}

func (c *Client) Remove(ctx context.Context, containerIDs []string) error {
	if len(containerIDs) == 0 {
		return nil
	}
	args := append([]string{}, c.dockerArgs...)
	args = append(args, "rm")
	args = append(args, containerIDs...)
	return c.run(ctx, args...)
}

func (c *Client) LogsTail(ctx context.Context, containerID string, tail int) (string, error) {
	args := append([]string{}, c.dockerArgs...)
	args = append(args, "logs", "--tail", fmt.Sprintf("%d", tail), containerID)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (c *Client) inspect(ctx context.Context, format string, containerID string) (string, error) {
	args := append([]string{}, c.dockerArgs...)
	args = append(args, "inspect", "--format="+format, containerID)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (c *Client) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
