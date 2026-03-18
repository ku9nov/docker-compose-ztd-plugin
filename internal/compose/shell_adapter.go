package compose

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type ShellAdapter struct {
	commandPrefix []string
}

func NewShellAdapter(dockerArgs []string) (*ShellAdapter, error) {
	if isCommandAvailable("docker", append(append([]string{}, dockerArgs...), "compose", "version")...) {
		prefix := append([]string{"docker"}, dockerArgs...)
		prefix = append(prefix, "compose")
		return &ShellAdapter{commandPrefix: prefix}, nil
	}

	if isCommandAvailable("docker-compose", "version") {
		return &ShellAdapter{commandPrefix: []string{"docker-compose"}}, nil
	}

	return nil, fmt.Errorf("docker compose or docker-compose is required")
}

func (s *ShellAdapter) Up(ctx context.Context, files []string, envFiles []string, service string, detached bool, noRecreate bool) error {
	args := []string{"up"}
	if detached {
		args = append(args, "--detach")
	}
	if noRecreate {
		args = append(args, "--no-recreate")
	}
	if service != "" {
		args = append(args, service)
	}
	return s.run(ctx, files, envFiles, args...)
}

func (s *ShellAdapter) Scale(ctx context.Context, files []string, envFiles []string, service string, replicas int) error {
	return s.run(ctx, files, envFiles, "up", "--detach", "--scale", service+"="+strconv.Itoa(replicas), "--no-recreate", service)
}

func (s *ShellAdapter) PsQuiet(ctx context.Context, files []string, envFiles []string, service string) ([]string, error) {
	args := []string{"ps", "--quiet"}
	if service != "" {
		args = append(args, service)
	}

	out, err := s.output(ctx, files, envFiles, args...)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var ids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

func (s *ShellAdapter) LogsFollowTail(ctx context.Context, files []string, service string, tail int) error {
	args := []string{"logs", "--follow", "--tail=" + strconv.Itoa(tail)}
	if service != "" {
		args = append(args, service)
	}
	return s.run(ctx, files, nil, args...)
}

func (s *ShellAdapter) run(ctx context.Context, files []string, envFiles []string, composeArgs ...string) error {
	allArgs := s.buildComposeArgs(files, envFiles, composeArgs...)
	cmd := exec.CommandContext(ctx, allArgs[0], allArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (s *ShellAdapter) output(ctx context.Context, files []string, envFiles []string, composeArgs ...string) (string, error) {
	allArgs := s.buildComposeArgs(files, envFiles, composeArgs...)
	cmd := exec.CommandContext(ctx, allArgs[0], allArgs[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (s *ShellAdapter) buildComposeArgs(files []string, envFiles []string, composeArgs ...string) []string {
	cmd := append([]string{}, s.commandPrefix...)
	for _, f := range files {
		cmd = append(cmd, "-f", f)
	}
	for _, env := range envFiles {
		cmd = append(cmd, "--env-file", env)
	}
	cmd = append(cmd, composeArgs...)
	return cmd
}

func isCommandAvailable(bin string, args ...string) bool {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
