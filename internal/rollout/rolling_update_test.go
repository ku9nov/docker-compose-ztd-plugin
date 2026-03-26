package rollout

import (
	"context"
	"errors"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
)

func TestDiffIDs(t *testing.T) {
	oldIDs := []string{"a", "b"}
	allIDs := []string{"a", "b", "c", "d"}
	got := diffIDs(oldIDs, allIDs)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("unexpected diff ids: %#v", got)
	}
}

func TestValidateProxyType(t *testing.T) {
	if err := validateProxyType("traefik"); err != nil {
		t.Fatalf("expected traefik to be valid, got error: %v", err)
	}

	if err := validateProxyType("nginx-proxy"); err == nil {
		t.Fatal("expected nginx-proxy to be rejected")
	}

	if err := validateProxyType("unknown"); err == nil {
		t.Fatal("expected unknown proxy to be rejected")
	}
}

type composeMock struct {
	psCalls int
}

func (m *composeMock) Up(context.Context, []string, []string, string, bool, bool) error { return nil }
func (m *composeMock) Scale(context.Context, []string, []string, string, int) error     { return nil }
func (m *composeMock) LogsFollowTail(context.Context, []string, string, int) error      { return nil }
func (m *composeMock) PsQuiet(_ context.Context, _ []string, _ []string, _ string) ([]string, error) {
	m.psCalls++
	if m.psCalls == 1 {
		return []string{"old-1", "old-2"}, nil
	}
	return []string{"old-1", "old-2", "new-1", "new-2"}, nil
}

type dockerMock struct {
	hasHealthcheckErr error
	stopCalls         [][]string
	removeCalls       [][]string
}

func (m *dockerMock) HasHealthcheck(context.Context, string) (bool, error) {
	if m.hasHealthcheckErr != nil {
		return false, m.hasHealthcheckErr
	}
	return true, nil
}
func (m *dockerMock) HealthStatus(context.Context, string) (string, error) { return "healthy", nil }
func (m *dockerMock) LogsTail(context.Context, string, int) (string, error) { return "", nil }
func (m *dockerMock) Stop(_ context.Context, ids []string) error {
	cp := append([]string{}, ids...)
	m.stopCalls = append(m.stopCalls, cp)
	return nil
}
func (m *dockerMock) Remove(_ context.Context, ids []string) error {
	cp := append([]string{}, ids...)
	m.removeCalls = append(m.removeCalls, cp)
	return nil
}

type generatorMock struct{}

func (m *generatorMock) Generate(context.Context, []string, []string, string) error { return nil }

func TestRun_RollbackGuardOnPostScaleError(t *testing.T) {
	t.Parallel()

	comp := &composeMock{}
	dock := &dockerMock{hasHealthcheckErr: errors.New("inspect failed")}

	var adapter compose.Adapter = comp
	updater := NewUpdater(logrus.New(), adapter, dock, &generatorMock{})

	err := updater.Run(context.Background(), Options{
		Service:              "svc",
		ComposeFiles:         []string{"docker-compose.yml"},
		ProxyType:            "traefik",
		HealthcheckTimeout:   1,
		NoHealthcheckTimeout: 1,
	})
	if err == nil {
		t.Fatal("expected error from healthcheck inspection")
	}

	if len(dock.stopCalls) != 1 {
		t.Fatalf("expected rollback guard to stop new containers once, got %d", len(dock.stopCalls))
	}
	if len(dock.removeCalls) != 1 {
		t.Fatalf("expected rollback guard to remove new containers once, got %d", len(dock.removeCalls))
	}

	if got := dock.stopCalls[0]; len(got) != 2 || got[0] != "new-1" || got[1] != "new-2" {
		t.Fatalf("unexpected stopped containers: %#v", got)
	}
	if got := dock.removeCalls[0]; len(got) != 2 || got[0] != "new-1" || got[1] != "new-2" {
		t.Fatalf("unexpected removed containers: %#v", got)
	}
}
