package cli

import (
	"fmt"
	"strconv"
	"strings"
)

func Parse(rawArgs []string) (Config, error) {
	cfg := Config{
		HealthcheckTimeout:   DefaultHealthcheckTimeout,
		NoHealthcheckTimeout: DefaultNoHealthcheckTimeout,
		WaitAfterHealthy:     DefaultWaitAfterHealthy,
		TraefikConfigFile:    DefaultTraefikConfig,
		ProxyType:            DefaultProxyType,
		Strategy:             DefaultStrategy,
		Weight:               DefaultCanaryWeight,
	}
	weightExplicitlySet := false

	args := rawArgs
	if ztdIdx := indexOf(args, "ztd"); ztdIdx >= 0 {
		cfg.DockerArgs = append(cfg.DockerArgs, args[:ztdIdx]...)
		args = args[ztdIdx+1:]
	}

	for len(args) > 0 {
		switch token := args[0]; {
		case token == "--proxy":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --proxy")
			}
			cfg.ProxyType = args[1]
			args = args[2:]
		case token == "--traefik-conf":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --traefik-conf")
			}
			cfg.TraefikConfigFile = args[1]
			args = args[2:]
		case token == "-h" || token == "--help":
			cfg.ShowHelp = true
			args = args[1:]
		case token == "-f" || token == "--file":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --file")
			}
			cfg.ComposeFiles = append(cfg.ComposeFiles, args[1])
			args = args[2:]
		case token == "--env-file":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --env-file")
			}
			cfg.EnvFiles = append(cfg.EnvFiles, args[1])
			args = args[2:]
		case token == "-t" || token == "--timeout":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --timeout")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return cfg, fmt.Errorf("invalid --timeout: %w", err)
			}
			cfg.HealthcheckTimeout = n
			args = args[2:]
		case token == "-w" || token == "--wait":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --wait")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return cfg, fmt.Errorf("invalid --wait: %w", err)
			}
			cfg.NoHealthcheckTimeout = n
			args = args[2:]
		case token == "--wait-after-healthy":
			if len(args) < 2 {
				return cfg, fmt.Errorf("missing value for --wait-after-healthy")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return cfg, fmt.Errorf("invalid --wait-after-healthy: %w", err)
			}
			cfg.WaitAfterHealthy = n
			args = args[2:]
		case token == "-d":
			if cfg.Service == "up" {
				cfg.UpDetached = true
				args = args[1:]
				continue
			}
			return cfg, fmt.Errorf("unknown option: -d")
		case token == "--strategy" || strings.HasPrefix(token, "--strategy="):
			value, consumed, err := parseStringFlag(args, "--strategy")
			if err != nil {
				return cfg, err
			}
			cfg.Strategy = value
			args = args[consumed:]
		case token == "--host-mode" || strings.HasPrefix(token, "--host-mode="):
			value, consumed, err := parseStringFlag(args, "--host-mode")
			if err != nil {
				return cfg, err
			}
			cfg.HostMode = value
			args = args[consumed:]
		case token == "--headers-mode" || strings.HasPrefix(token, "--headers-mode="):
			value, consumed, err := parseStringFlag(args, "--headers-mode")
			if err != nil {
				return cfg, err
			}
			cfg.HeadersMode = value
			args = args[consumed:]
		case token == "--cookies-mode" || strings.HasPrefix(token, "--cookies-mode="):
			value, consumed, err := parseStringFlag(args, "--cookies-mode")
			if err != nil {
				return cfg, err
			}
			cfg.CookiesMode = value
			args = args[consumed:]
		case token == "--ip-mode" || strings.HasPrefix(token, "--ip-mode="):
			value, consumed, err := parseStringFlag(args, "--ip-mode")
			if err != nil {
				return cfg, err
			}
			cfg.IPMode = value
			args = args[consumed:]
		case token == "--weight" || strings.HasPrefix(token, "--weight="):
			value, consumed, err := parseIntFlag(args, "--weight")
			if err != nil {
				return cfg, err
			}
			cfg.Weight = value
			weightExplicitlySet = true
			args = args[consumed:]
		default:
			if len(token) > 0 && token[0] == '-' {
				return cfg, fmt.Errorf("unknown option: %s", token)
			}

			if cfg.Service != "" && cfg.Service != token {
				return cfg, fmt.Errorf("SERVICE is already set to '%s'", cfg.Service)
			}

			cfg.Service = token
			args = args[1:]
		}
	}

	if err := validateStrategy(cfg, weightExplicitlySet); err != nil {
		return cfg, err
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

func parseStringFlag(args []string, flag string) (string, int, error) {
	if inline, ok := parseInlineValue(args[0], flag); ok {
		if inline == "" {
			return "", 0, fmt.Errorf("missing value for %s", flag)
		}
		return inline, 1, nil
	}
	if len(args) < 2 || args[1] == "" {
		return "", 0, fmt.Errorf("missing value for %s", flag)
	}
	return args[1], 2, nil
}

func parseIntFlag(args []string, flag string) (int, int, error) {
	raw, consumed, err := parseStringFlag(args, flag)
	if err != nil {
		return 0, 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid %s: %w", flag, err)
	}
	return n, consumed, nil
}

func parseInlineValue(token string, flag string) (string, bool) {
	prefix := flag + "="
	if strings.HasPrefix(token, prefix) {
		return token[len(prefix):], true
	}
	return "", false
}

func validateStrategy(cfg Config, weightExplicitlySet bool) error {
	switch cfg.Strategy {
	case StrategyRolling:
	case StrategyBlueGreen:
	case StrategyCanary:
	default:
		return fmt.Errorf("invalid --strategy: %s", cfg.Strategy)
	}

	if cfg.Strategy != StrategyBlueGreen {
		if cfg.HostMode != "" || cfg.HeadersMode != "" || cfg.CookiesMode != "" || cfg.IPMode != "" {
			return fmt.Errorf("--host-mode, --headers-mode, --cookies-mode and --ip-mode require --strategy=%s", StrategyBlueGreen)
		}
	}

	if cfg.Strategy != StrategyCanary && weightExplicitlySet {
		return fmt.Errorf("--weight requires --strategy=%s", StrategyCanary)
	}
	if cfg.Strategy == StrategyCanary && (cfg.Weight <= 0 || cfg.Weight > 100) {
		return fmt.Errorf("--weight must be between 1 and 100 for --strategy=%s", StrategyCanary)
	}

	return nil
}
