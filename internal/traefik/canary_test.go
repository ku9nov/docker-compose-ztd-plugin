package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

func TestApplyCanaryConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "dynamic_conf.yml")
	err := ApplyCanaryConfig(path, CanaryConfigInput{
		Service:        "api",
		ProductionRule: "Host(`example.com`)",
		Port:           "8080",
		OldIDs:         []string{"aaaaaaaaaaaa111111111111"},
		NewIDs:         []string{"bbbbbbbbbbbb222222222222"},
		NewWeight:      10,
		TCPRouters: []TCPRouteInput{
			{
				RouterName:      "api-xmpp",
				Rule:            "HostSNI(`*`)",
				RouterService:   "api-xmpp",
				BackendPort:     "5222",
				BackendBaseName: "api-xmpp",
				EntryPoints:     []string{"xmpp"},
			},
		},
		HealthCheck: &types.HealthChecks{
			Path: "/healthz",
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
	assertContains(t, content, "api_old")
	assertContains(t, content, "api_new")
	assertContains(t, content, "weighted:")
	assertContains(t, content, "name: api_old")
	assertContains(t, content, "weight: 90")
	assertContains(t, content, "name: api_new")
	assertContains(t, content, "weight: 10")
	assertContains(t, content, "path: /healthz")
	assertContains(t, content, "tcp:")
	assertContains(t, content, "api-xmpp_old:")
	assertContains(t, content, "api-xmpp_new:")
	assertContains(t, content, "name: api-xmpp_old")
	assertContains(t, content, "name: api-xmpp_new")
	assertContains(t, content, "address: aaaaaaaaaaaa:5222")
	assertContains(t, content, "service: api-xmpp")
}

func TestApplyCanaryConfigTerminalWeightAllowsSingleSide(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "dynamic_conf.yml")
	err := ApplyCanaryConfig(path, CanaryConfigInput{
		Service:        "api",
		ProductionRule: "Host(`example.com`)",
		OldIDs:         nil,
		NewIDs:         []string{"bbbbbbbbbbbb222222222222"},
		NewWeight:      100,
	})
	if err != nil {
		t.Fatalf("apply config: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "name: api_old") {
		t.Fatalf("did not expect old weighted service in config:\n%s", content)
	}
	assertContains(t, content, "name: api_new")
	assertContains(t, content, "weight: 100")
}

func TestApplyCanaryConfig_OmitsTCPSectionWhenNoTCPRouters(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "dynamic_conf.yml")
	err := ApplyCanaryConfig(path, CanaryConfigInput{
		Service:        "api",
		ProductionRule: "Host(`example.com`)",
		Port:           "8080",
		OldIDs:         []string{"aaaaaaaaaaaa111111111111"},
		NewIDs:         []string{"bbbbbbbbbbbb222222222222"},
		NewWeight:      10,
	})
	if err != nil {
		t.Fatalf("apply config: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	assertNotContains(t, content, "\ntcp:")
	if strings.HasPrefix(content, "tcp:") {
		t.Fatalf("expected no tcp section in generated config, got:\n%s", content)
	}
}
