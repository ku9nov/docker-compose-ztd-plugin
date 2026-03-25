package cli

import (
	"testing"
	"time"
)

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
		"--headers-mode=X-Env=green",
		"--cookies-mode=env=green",
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

func TestParse_BlueGreenHeadersModeInvalidFormatFails(t *testing.T) {
	_, err := Parse([]string{
		"--strategy=blue-green",
		"--headers-mode=Headers(`X-Env`, `green`)",
		"example",
	})
	if err == nil {
		t.Fatal("expected parse error for invalid headers-mode format")
	}
}

func TestParse_BlueGreenCookiesModeInvalidFormatFails(t *testing.T) {
	_, err := Parse([]string{
		"--strategy=blue-green",
		"--cookies-mode=HeadersRegexp(`Cookie`, `env=green`)",
		"example",
	})
	if err == nil {
		t.Fatal("expected parse error for invalid cookies-mode format")
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

func TestParse_BlueGreenSwitchActionWithAutoCleanup(t *testing.T) {
	cfg, err := Parse([]string{
		"--strategy=blue-green",
		"--auto-cleanup=10m",
		"--to=green",
		"api",
		"switch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Service != "api" {
		t.Fatalf("expected api service, got %s", cfg.Service)
	}
	if cfg.Action != ActionSwitch {
		t.Fatalf("expected action switch, got %s", cfg.Action)
	}
	if cfg.SwitchTo != "green" {
		t.Fatalf("expected switch target green, got %s", cfg.SwitchTo)
	}
	if cfg.AutoCleanup != 10*time.Minute {
		t.Fatalf("expected auto cleanup 10m, got %s", cfg.AutoCleanup)
	}
}

func TestParse_ActionDefaultsStrategyToBlueGreen(t *testing.T) {
	cfg, err := Parse([]string{
		"api",
		"cleanup",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Strategy != StrategyBlueGreen {
		t.Fatalf("expected strategy %s, got %s", StrategyBlueGreen, cfg.Strategy)
	}
	if cfg.Action != ActionCleanup {
		t.Fatalf("expected cleanup action, got %s", cfg.Action)
	}
}

func TestParse_AutoCleanupRequiresSwitch(t *testing.T) {
	_, err := Parse([]string{
		"--strategy=blue-green",
		"--auto-cleanup=10m",
		"api",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParse_ToRequiresSwitch(t *testing.T) {
	_, err := Parse([]string{
		"--strategy=blue-green",
		"--to=green",
		"api",
		"cleanup",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParse_CanaryRollbackDefaultsStrategyToCanary(t *testing.T) {
	cfg, err := Parse([]string{
		"api",
		"rollback",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Strategy != StrategyCanary {
		t.Fatalf("expected strategy %s, got %s", StrategyCanary, cfg.Strategy)
	}
	if cfg.Action != ActionRollback {
		t.Fatalf("expected action rollback, got %s", cfg.Action)
	}
}

func TestParse_PromoteActionIsNotSupported(t *testing.T) {
	_, err := Parse([]string{
		"api",
		"promote",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParse_AutoCleanupAllowsCanaryRollback(t *testing.T) {
	cfg, err := Parse([]string{
		"--auto-cleanup=10m",
		"api",
		"rollback",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Action != ActionRollback {
		t.Fatalf("expected action rollback, got %s", cfg.Action)
	}
	if cfg.AutoCleanup != 10*time.Minute {
		t.Fatalf("expected auto cleanup 10m, got %s", cfg.AutoCleanup)
	}
}

func TestParse_AnalyzeFlags(t *testing.T) {
	cfg, err := Parse([]string{
		"--strategy=canary",
		"--analyze",
		"--metrics-url=http://localhost:8080/metrics",
		"--analyze-window=45s",
		"--analyze-interval=5s",
		"--min-requests=120",
		"--max-5xx-ratio=0.02",
		"--max-4xx-ratio=0.10",
		"--max-mean-latency-ms=250",
		"api",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Analyze {
		t.Fatal("expected analyze enabled")
	}
	if cfg.AnalyzeWindow != 45*time.Second || cfg.AnalyzeInterval != 5*time.Second {
		t.Fatalf("unexpected analyze timing config: window=%s interval=%s", cfg.AnalyzeWindow, cfg.AnalyzeInterval)
	}
	if cfg.AnalyzeMinRequests != 120 {
		t.Fatalf("expected min requests 120, got %d", cfg.AnalyzeMinRequests)
	}
	if cfg.AnalyzeMax5xxRatio != 0.02 || cfg.AnalyzeMax4xxRatio != 0.10 || cfg.AnalyzeMaxLatencyMS != 250 {
		t.Fatalf("unexpected analyze thresholds: %+v", cfg)
	}
}

func TestParse_AnalyzeRequiresBlueGreenOrCanary(t *testing.T) {
	_, err := Parse([]string{
		"--strategy=rolling",
		"--analyze",
		"api",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParse_AllFlagIsUnknown(t *testing.T) {
	_, err := Parse([]string{
		"--all",
		"api",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}
