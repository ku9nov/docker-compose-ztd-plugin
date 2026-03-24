package bluegreen

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/metricsgate"
	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
)

type dockerMock struct {
	labels map[string]string
	stop   []string
	remove []string
}

type composeMock struct {
	idsByService map[string][]string
	scaleCalls   int
}

func (m *composeMock) Up(context.Context, []string, []string, string, bool, bool) error { return nil }
func (m *composeMock) Scale(context.Context, []string, []string, string, int) error {
	m.scaleCalls++
	return nil
}
func (m *composeMock) PsQuiet(_ context.Context, _ []string, _ []string, service string) ([]string, error) {
	return append([]string{}, m.idsByService[service]...), nil
}
func (m *composeMock) LogsFollowTail(context.Context, []string, string, int) error { return nil }

func (m *dockerMock) HasHealthcheck(context.Context, string) (bool, error) { return true, nil }
func (m *dockerMock) HealthStatus(context.Context, string) (string, error) { return "healthy", nil }
func (m *dockerMock) Stop(_ context.Context, ids []string) error {
	m.stop = append(m.stop, ids...)
	return nil
}
func (m *dockerMock) Remove(_ context.Context, ids []string) error {
	m.remove = append(m.remove, ids...)
	return nil
}
func (m *dockerMock) Labels(context.Context, string) (map[string]string, error) { return m.labels, nil }

func TestSwitchTrafficUpdatesState(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
		GreenStats: &state.MetricsBaseline{
			CapturedAt:    time.Now().UTC(),
			Requests2xx:   10,
			Requests4xx:   0,
			Requests5xx:   0,
			DurationSum:   1,
			DurationCount: 10,
		},
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	deployer := NewDeployer(logrus.New(), nil, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                           "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port":      "8080",
			"com.docker.compose.project":                              "project",
			"traefik.http.services.api.loadbalancer.healthCheck.path": "/health",
		},
	}, store)

	err := deployer.switchTraffic(context.Background(), Options{
		Service:           "api",
		SwitchTo:          state.ColorGreen,
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
		AutoCleanup:       10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("switch traffic failed: %v", err)
	}

	got, err := store.Load("project")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got.Active != state.ColorGreen {
		t.Fatalf("expected active green, got %s", got.Active)
	}
	if got.CleanupAt == nil {
		t.Fatal("expected cleanupAt after --auto-cleanup switch")
	}
}

func TestCleanupInactiveRemovesState(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorGreen,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	dock := &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}
	deployer := NewDeployer(logrus.New(), nil, dock, store)
	if err := deployer.cleanupInactive(context.Background(), Options{
		Service:           "api",
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
	}); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if len(dock.stop) != 1 || dock.stop[0] != "blue-id" {
		t.Fatalf("expected stop blue-id, got %#v", dock.stop)
	}
	if len(dock.remove) != 1 || dock.remove[0] != "blue-id" {
		t.Fatalf("expected remove blue-id, got %#v", dock.remove)
	}
	if _, err := store.Load("project"); err == nil {
		t.Fatal("expected state to be deleted after cleanup")
	}
}

func TestDeployBlockedWhenBlueGreenStateExists(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
		GreenStats: &state.MetricsBaseline{
			CapturedAt:    time.Now().UTC(),
			Requests2xx:   10,
			Requests4xx:   0,
			Requests5xx:   0,
			DurationSum:   1,
			DurationCount: 10,
		},
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	compose := &composeMock{
		idsByService: map[string][]string{
			"api": {"blue-id", "green-id"},
		},
	}
	deployer := NewDeployer(logrus.New(), compose, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)

	err := deployer.deploy(context.Background(), Options{
		Service:           "api",
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
	})
	if err == nil {
		t.Fatal("expected deploy to be blocked when state exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	if compose.scaleCalls != 0 {
		t.Fatalf("expected no scale calls, got %d", compose.scaleCalls)
	}
}

func TestDeployWithAnalyzeRunsObserveOnlyWhenStateExists(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
		GreenStats: &state.MetricsBaseline{
			CapturedAt:    time.Now().UTC(),
			Requests2xx:   10,
			Requests4xx:   0,
			Requests5xx:   0,
			DurationSum:   1,
			DurationCount: 10,
		},
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		if call == 1 {
			fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api-green@file"} 10`)
			return
		}
		fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api-green@file"} 20`)
	}))
	defer server.Close()

	compose := &composeMock{
		idsByService: map[string][]string{
			"api": {"blue-id", "green-id"},
		},
	}
	deployer := NewDeployer(logrus.New(), compose, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)

	started := time.Now()
	err := deployer.deploy(context.Background(), Options{
		Service:           "api",
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
		Metrics: metricsgate.Config{
			Enabled:          true,
			URL:              server.URL,
			Window:           2 * time.Second,
			Interval:         10 * time.Millisecond,
			MinRequests:      0,
			Max5xxRatio:      0.05,
			Max4xxRatio:      -1,
			MaxMeanLatencyMS: -1,
		},
	})
	if err != nil {
		t.Fatalf("expected analyze-only mode to succeed, got: %v", err)
	}
	if compose.scaleCalls != 0 {
		t.Fatalf("expected no scale calls in analyze-only mode, got %d", compose.scaleCalls)
	}
	if time.Since(started) > time.Second {
		t.Fatal("expected lifetime analysis without waiting full window")
	}
}

func TestDeployFailsSafeOnBlueGreenStateDrift(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	compose := &composeMock{
		idsByService: map[string][]string{
			"api": {"blue-id"},
		},
	}
	deployer := NewDeployer(logrus.New(), compose, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)

	err := deployer.deploy(context.Background(), Options{
		Service:           "api",
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
	})
	if err == nil {
		t.Fatal("expected fail-safe error on blue-green state drift")
	}
	if !strings.Contains(err.Error(), "state drift") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSwitchTrafficAnalyzeFailureIsWarningOnly(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		if call == 1 {
			fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api-green@file"} 100`)
			fmt.Fprintln(w, `traefik_service_requests_total{code="500",service="api-green@file"} 1`)
			return
		}
		fmt.Fprintln(w, `traefik_service_requests_total{code="200",service="api-green@file"} 110`)
		fmt.Fprintln(w, `traefik_service_requests_total{code="500",service="api-green@file"} 20`)
	}))
	defer server.Close()

	deployer := NewDeployer(logrus.New(), nil, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)

	err := deployer.switchTraffic(context.Background(), Options{
		Service:           "api",
		SwitchTo:          state.ColorGreen,
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
		Metrics: metricsgate.Config{
			Enabled:          true,
			URL:              server.URL,
			Window:           40 * time.Millisecond,
			Interval:         10 * time.Millisecond,
			MinRequests:      5,
			Max5xxRatio:      0.05,
			Max4xxRatio:      -1,
			MaxMeanLatencyMS: -1,
		},
	})
	if err != nil {
		t.Fatalf("expected warning-only analyze behavior, got error: %v", err)
	}
}

func TestSwitchTrafficWritesTCPDynamicConfig(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-id"},
		Green:     []string{"green-id"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
		QA: &state.QAModes{
			Host: "green.example.com",
			IP:   "10.0.0.0/24",
		},
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	dynamicPath := t.TempDir() + "/dynamic.yml"
	deployer := NewDeployer(logrus.New(), nil, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                           "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port":      "8080",
			"traefik.tcp.routers.api-xmpp.rule":                       "HostSNI(`*`)",
			"traefik.tcp.routers.api-xmpp.entrypoints":                "xmpp",
			"traefik.tcp.services.api-xmpp.loadbalancer.server.port":  "5222",
			"traefik.tcp.routers.api-xmpp.service":                    "api-xmpp",
			"traefik.http.services.api.loadbalancer.healthCheck.path": "/health",
		},
	}, store)

	if err := deployer.switchTraffic(context.Background(), Options{
		Service:           "api",
		SwitchTo:          state.ColorGreen,
		TraefikConfigFile: dynamicPath,
	}); err != nil {
		t.Fatalf("switch traffic failed: %v", err)
	}

	data, err := os.ReadFile(dynamicPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "\ntcp:") && !strings.HasPrefix(content, "tcp:") {
		t.Fatalf("expected tcp section in config, got:\n%s", content)
	}
	if !strings.Contains(content, "api-xmpp-blue") || !strings.Contains(content, "api-xmpp-green") {
		t.Fatalf("expected blue/green tcp services in config, got:\n%s", content)
	}
	if !strings.Contains(content, "api-qa-api-xmpp-tcp-host") || !strings.Contains(content, "api-qa-api-xmpp-tcp-ip") {
		t.Fatalf("expected tcp QA routers in config, got:\n%s", content)
	}
}
