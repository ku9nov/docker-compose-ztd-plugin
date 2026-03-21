package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

func TestApplyBlueGreenConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "dynamic_conf.yml")
	err := ApplyBlueGreenConfig(path, BlueGreenConfigInput{
		Service:        "api",
		Active:         state.ColorBlue,
		ProductionRule: "Host(`example.com`)",
		Port:           "8080",
		BlueIDs:        []string{"aaaaaaaaaaaa111111111111"},
		GreenIDs:       []string{"bbbbbbbbbbbb222222222222"},
		QA: &state.QAModes{
			Host:    "green.example.com",
			Headers: "X-Env=green",
			Cookies: "env=green",
			IP:      "10.0.0.0/24",
		},
		HealthCheck: &types.HealthChecks{
			Path:    "/health",
			Headers: map[string]string{"My-Custom-Header": "foo"},
		},
	})
	if err != nil {
		t.Fatalf("apply config: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	assertContains(t, content, "api-blue")
	assertContains(t, content, "api-green")
	assertContains(t, content, "api-qa-host")
	assertContains(t, content, "priority: 100")
	assertContains(t, content, "ClientIP(`10.0.0.0/24`)")
	assertContains(t, content, "Header(`X-Env`, `green`)")
	assertContains(t, content, "HeaderRegexp(`Cookie`, `(^|;\\s*)env=green(;|$)`)")
	assertContains(t, content, "My-Custom-Header: foo")
	assertContains(t, content, "api-blue:")
	assertContains(t, content, "api-green:")
	assertNotContains(t, content, "api:\n            loadBalancer")
}

func assertContains(t *testing.T, content string, expected string) {
	t.Helper()
	if !strings.Contains(content, expected) {
		t.Fatalf("expected content to include %q\n%s", expected, content)
	}
}

func assertNotContains(t *testing.T, content string, unexpected string) {
	t.Helper()
	if strings.Contains(content, unexpected) {
		t.Fatalf("expected content not to include %q\n%s", unexpected, content)
	}
}
