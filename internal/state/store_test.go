package state

import (
	"testing"
	"time"
)

func TestStoreSaveLoadDelete(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	now := time.Now().UTC()
	st := DeploymentState{
		Service:   "api",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"a"},
		Green:     []string{"b"},
		Active:    ColorBlue,
		CreatedAt: now,
	}

	if err := store.Save("project", st); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	got, err := store.Load("project")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got.Service != "api" || got.Active != ColorBlue {
		t.Fatalf("unexpected loaded state: %+v", got)
	}

	if err := store.Delete("project"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.Load("project"); err == nil {
		t.Fatal("expected load error after delete")
	}
}

func TestResolveProjectName(t *testing.T) {
	t.Parallel()

	got, err := ResolveProjectName(map[string]string{
		"com.docker.compose.project": "my-stack",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-stack" {
		t.Fatalf("unexpected project: %s", got)
	}
}

func TestServiceStateKey(t *testing.T) {
	t.Parallel()

	key, err := ServiceStateKey("docker-compose-deployments-examples", "helloworld-front")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "docker-compose-deployments-examples--helloworld-front" {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestStoreKeepsMultipleServiceStates(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	now := time.Now().UTC()
	s1 := DeploymentState{
		Service:   "helloworld",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"a"},
		Green:     []string{"b"},
		Active:    ColorBlue,
		CreatedAt: now,
	}
	s2 := DeploymentState{
		Service:   "helloworld-front",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"c"},
		Green:     []string{"d"},
		Active:    ColorBlue,
		CreatedAt: now,
	}

	k1, err := ServiceStateKey("example", s1.Service)
	if err != nil {
		t.Fatalf("key1 error: %v", err)
	}
	k2, err := ServiceStateKey("example", s2.Service)
	if err != nil {
		t.Fatalf("key2 error: %v", err)
	}

	if err := store.Save(k1, s1); err != nil {
		t.Fatalf("save s1 failed: %v", err)
	}
	if err := store.Save(k2, s2); err != nil {
		t.Fatalf("save s2 failed: %v", err)
	}

	projects, err := store.ListProjects()
	if err != nil {
		t.Fatalf("list states failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 state files, got %d (%v)", len(projects), projects)
	}
}
