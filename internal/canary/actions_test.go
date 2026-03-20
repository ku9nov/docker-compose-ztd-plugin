package canary

import (
	"context"
	"testing"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
	"github.com/sirupsen/logrus"
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

func TestCleanupRejectsNonTerminalWeight(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyCanary,
		Old:       []string{"old-id"},
		New:       []string{"new-id"},
		Weight:    30,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	deployer := NewDeployer(logrus.New(), nil, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)
	err := deployer.cleanup(context.Background(), Options{
		Service:           "api",
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
	})
	if err == nil {
		t.Fatal("expected cleanup error for non-terminal canary state")
	}
}

func TestRollbackSetsZeroWeightAndAutoCleanup(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyCanary,
		Old:       []string{"old-id"},
		New:       []string{"new-id"},
		Weight:    70,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	deployer := NewDeployer(logrus.New(), nil, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)
	if err := deployer.rollback(context.Background(), Options{
		Service:           "api",
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
		AutoCleanup:       10 * time.Minute,
	}); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	got, err := store.Load("project")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got.Weight != 0 {
		t.Fatalf("expected weight 0, got %d", got.Weight)
	}
	if got.CleanupAt == nil {
		t.Fatal("expected cleanupAt after rollback with auto-cleanup")
	}
}

func TestDeployReusesExistingCanaryStateWithoutScaling(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyCanary,
		Old:       []string{"old-id"},
		New:       []string{"new-id"},
		Weight:    0,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	compose := &composeMock{
		idsByService: map[string][]string{
			"api": {"old-id", "new-id"},
		},
	}
	deployer := NewDeployer(logrus.New(), compose, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)
	if err := deployer.deploy(context.Background(), Options{
		Service:           "api",
		Weight:            30,
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
	}); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}
	if compose.scaleCalls != 0 {
		t.Fatalf("expected no scaling when canary state exists, got %d scale calls", compose.scaleCalls)
	}

	got, err := store.Load("project")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got.Weight != 30 {
		t.Fatalf("expected weight 30, got %d", got.Weight)
	}
}

func TestDeployFailsSafeOnStateDrift(t *testing.T) {
	t.Parallel()

	store := state.NewStore(t.TempDir())
	st := state.DeploymentState{
		Service:   "api",
		Strategy:  state.StrategyCanary,
		Old:       []string{"old-id"},
		New:       []string{"new-id"},
		Weight:    0,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("project", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	compose := &composeMock{
		idsByService: map[string][]string{
			"api": {"old-id"},
		},
	}
	deployer := NewDeployer(logrus.New(), compose, &dockerMock{
		labels: map[string]string{
			"traefik.http.routers.api.rule":                      "Host(`example.com`)",
			"traefik.http.services.api.loadbalancer.server.port": "8080",
		},
	}, store)
	if err := deployer.deploy(context.Background(), Options{
		Service:           "api",
		Weight:            30,
		TraefikConfigFile: t.TempDir() + "/dynamic.yml",
	}); err == nil {
		t.Fatal("expected fail-safe error when state drifts from running containers")
	}
}
