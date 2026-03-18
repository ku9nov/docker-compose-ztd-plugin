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
