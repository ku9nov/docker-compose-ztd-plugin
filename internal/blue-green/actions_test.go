package bluegreen

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
)

type dockerMock struct {
	labels map[string]string
	stop   []string
	remove []string
}

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
