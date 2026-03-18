package traefik

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
)

type composeMock struct{}

func (m *composeMock) Up(context.Context, []string, []string, string, bool, bool) error { return nil }
func (m *composeMock) Scale(context.Context, []string, []string, string, int) error     { return nil }
func (m *composeMock) LogsFollowTail(context.Context, []string, string, int) error      { return nil }

func (m *composeMock) PsQuiet(_ context.Context, _ []string, _ []string, service string) ([]string, error) {
	switch service {
	case "example":
		return []string{"abcdef1234567890", "fedcba6543219999"}, nil
	case "":
		return []string{"abcdef1234567890", "fedcba6543219999"}, nil
	default:
		return nil, nil
	}
}

type dockerMock struct{}

func (m *dockerMock) Labels(_ context.Context, containerID string) (map[string]string, error) {
	base := map[string]string{
		"com.docker.compose.service": "example",
	}
	if containerID == "abcdef1234567890" {
		base["traefik.http.routers.example.rule"] = "Host(`example.com`) && PathPrefix(`/`)"
		base["traefik.http.services.example.loadbalancer.server.port"] = "9001"
		base["traefik.http.services.example.loadbalancer.healthCheck.path"] = "/health"
		base["traefik.http.services.example.loadbalancer.healthCheck.interval"] = "10s"
		base["traefik.http.services.example.loadbalancer.healthCheck.timeout"] = "1s"
		base["traefik.tcp.routers.example-xmpp.rule"] = "HostSNI(`*`)"
		base["traefik.tcp.routers.example-xmpp.entrypoints"] = "xmpp"
		base["traefik.tcp.services.example-xmpp.loadbalancer.server.port"] = "5222"
	}
	return base, nil
}

type dockerNoTCPMock struct{}

func (m *dockerNoTCPMock) Labels(_ context.Context, containerID string) (map[string]string, error) {
	base := map[string]string{
		"com.docker.compose.service": "example",
	}
	if containerID == "abcdef1234567890" {
		base["traefik.http.routers.example.rule"] = "Host(`example.com`) && PathPrefix(`/`)"
		base["traefik.http.services.example.loadbalancer.server.port"] = "9001"
		base["traefik.http.services.example.loadbalancer.healthCheck.path"] = "/health"
		base["traefik.http.services.example.loadbalancer.healthCheck.interval"] = "10s"
		base["traefik.http.services.example.loadbalancer.healthCheck.timeout"] = "1s"
	}
	return base, nil
}

func TestGenerate_GoldenConfig(t *testing.T) {
	t.Parallel()

	root := "testdata"
	composePath := filepath.Join(root, "compose.yml")
	goldenPath := filepath.Join(root, "dynamic_conf.golden.yml")
	outputPath := filepath.Join(t.TempDir(), "dynamic_conf.yml")

	gen := NewGenerator(&composeMock{}, &dockerMock{})
	if err := gen.Generate(context.Background(), []string{composePath}, nil, outputPath); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	gotRaw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	wantRaw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	gotCanonical, err := canonicalYAML(gotRaw)
	if err != nil {
		t.Fatalf("canonicalize generated yaml: %v", err)
	}
	wantCanonical, err := canonicalYAML(wantRaw)
	if err != nil {
		t.Fatalf("canonicalize golden yaml: %v", err)
	}

	if gotCanonical != wantCanonical {
		t.Fatalf("generated yaml differs from golden\nwant=%s\ngot=%s", wantCanonical, gotCanonical)
	}
}

func TestGenerate_OmitsEmptyTCPSection(t *testing.T) {
	t.Parallel()

	root := "testdata"
	composePath := filepath.Join(root, "compose.yml")
	outputPath := filepath.Join(t.TempDir(), "dynamic_conf.yml")

	gen := NewGenerator(&composeMock{}, &dockerNoTCPMock{})
	if err := gen.Generate(context.Background(), []string{composePath}, nil, outputPath); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	gotRaw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	content := string(gotRaw)
	if strings.Contains(content, "\ntcp:") || strings.HasPrefix(content, "tcp:") {
		t.Fatalf("expected no tcp section in generated config, got:\n%s", content)
	}
}

func canonicalYAML(data []byte) (string, error) {
	var v any
	if err := configio.UnmarshalYAML(data, &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
