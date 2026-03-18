package cli

import (
	"fmt"
	"strconv"
)

func Parse(rawArgs []string) (Config, error) {
	cfg := Config{
		HealthcheckTimeout:   DefaultHealthcheckTimeout,
		NoHealthcheckTimeout: DefaultNoHealthcheckTimeout,
		WaitAfterHealthy:     DefaultWaitAfterHealthy,
		TraefikConfigFile:    DefaultTraefikConfig,
		ProxyType:            DefaultProxyType,
	}

	args := rawArgs
	if ztdIdx := indexOf(args, "ztd"); ztdIdx >= 0 {
		cfg.DockerArgs = append(cfg.DockerArgs, args[:ztdIdx]...)
		args = args[ztdIdx+1:]
	}

	for len(args) > 0 {
		switch args[0] {
		case "--proxy":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --proxy")
			}
			cfg.ProxyType = args[1]
			args = args[2:]
		case "--traefik-conf":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --traefik-conf")
			}
			cfg.TraefikConfigFile = args[1]
			args = args[2:]
		case "-h", "--help":
			cfg.ShowHelp = true
			args = args[1:]
		case "-f", "--file":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --file")
			}
			cfg.ComposeFiles = append(cfg.ComposeFiles, args[1])
			args = args[2:]
		case "--env-file":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --env-file")
			}
			cfg.EnvFiles = append(cfg.EnvFiles, args[1])
			args = args[2:]
		case "-t", "--timeout":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --timeout")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return cfg, fmt.Errorf("invalid --timeout: %w", err)
			}
			cfg.HealthcheckTimeout = n
			args = args[2:]
		case "-w", "--wait":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --wait")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return cfg, fmt.Errorf("invalid --wait: %w", err)
			}
			cfg.NoHealthcheckTimeout = n
			args = args[2:]
		case "--wait-after-healthy":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --wait-after-healthy")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return cfg, fmt.Errorf("invalid --wait-after-healthy: %w", err)
			}
			cfg.WaitAfterHealthy = n
			args = args[2:]
		case "-d":
			if cfg.Service == "up" {
				cfg.UpDetached = true
				args = args[1:]
				continue
			}
			return cfg, fmt.Errorf("unknown option: -d")
		default:
			if len(args[0]) > 0 && args[0][0] == '-' {
				return cfg, fmt.Errorf("unknown option: %s", args[0])
			}

			if cfg.Service != "" && cfg.Service != args[0] {
				return cfg, fmt.Errorf("SERVICE is already set to '%s'", cfg.Service)
			}

			cfg.Service = args[0]
			args = args[1:]
		}
	}

	return cfg, nil
}

func indexOf(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}
