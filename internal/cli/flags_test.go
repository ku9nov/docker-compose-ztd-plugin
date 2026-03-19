package cli

import "testing"

func TestParse_ServiceAndFlags(t *testing.T) {
	cfg, err := Parse([]string{
		"--context", "prod", "ztd",
		"-f", "docker-compose.yml",
		"--env-file", ".env.prod",
		"--proxy", "traefik",
		"--traefik-conf", "traefik/dynamic_conf.yml",
		"-t", "90",
		"-w", "20",
		"--wait-after-healthy", "5",
		"example",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Service != "example" {
		t.Fatalf("expected service example, got %s", cfg.Service)
	}
	if cfg.HealthcheckTimeout != 90 || cfg.NoHealthcheckTimeout != 20 || cfg.WaitAfterHealthy != 5 {
		t.Fatalf("unexpected timeout values: %+v", cfg)
	}
	if len(cfg.DockerArgs) != 2 || cfg.DockerArgs[0] != "--context" || cfg.DockerArgs[1] != "prod" {
		t.Fatalf("unexpected docker args: %#v", cfg.DockerArgs)
	}
	if cfg.Strategy != StrategyRolling {
		t.Fatalf("expected default strategy %s, got %s", StrategyRolling, cfg.Strategy)
	}
}

func TestParse_UpDetached(t *testing.T) {
	cfg, err := Parse([]string{"up", "-d"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Service != "up" || !cfg.UpDetached {
		t.Fatalf("expected up -d, got %+v", cfg)
	}
}

func TestParse_UnknownFlag(t *testing.T) {
	_, err := Parse([]string{"--invalid", "svc"})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParse_BlueGreenStrategyWithModes(t *testing.T) {
	cfg, err := Parse([]string{
		"--strategy=blue-green",
		"--host-mode=green.example.com",
		"--headers-mode=Headers(`X-Env`, `green`)",
		"--cookies-mode=HeadersRegexp(`Cookie`, `env=green`)",
		"--ip-mode=10.0.0.0/24",
		"example",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Strategy != StrategyBlueGreen {
		t.Fatalf("expected strategy %s, got %s", StrategyBlueGreen, cfg.Strategy)
	}
	if cfg.HostMode != "green.example.com" || cfg.IPMode != "10.0.0.0/24" {
		t.Fatalf("unexpected blue-green flags: %+v", cfg)
	}
}

func TestParse_CanaryStrategyWithWeight(t *testing.T) {
	cfg, err := Parse([]string{
		"--strategy", "canary",
		"--weight=10",
		"example",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Strategy != StrategyCanary {
		t.Fatalf("expected strategy %s, got %s", StrategyCanary, cfg.Strategy)
	}
	if cfg.Weight != 10 {
		t.Fatalf("expected weight 10, got %d", cfg.Weight)
	}
}

func TestParse_WeightWithoutCanaryFails(t *testing.T) {
	_, err := Parse([]string{
		"--strategy=rolling",
		"--weight=10",
		"example",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}
