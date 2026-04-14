package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupWorkerProcessOverdue(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	due := time.Now().UTC().Add(-time.Minute)
	if err := store.Save("proj", DeploymentState{
		Service:   "api",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"a"},
		Green:     []string{"b"},
		Active:    ColorBlue,
		CreatedAt: time.Now().UTC(),
		CleanupAt: &due,
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	var called bool
	worker := NewCleanupWorker(store, func(_ context.Context, project string, st DeploymentState) error {
		called = true
		if project != "proj" || st.Service != "api" {
			t.Fatalf("unexpected cleanup call args: project=%s state=%+v", project, st)
		}
		return nil
	})

	if err := worker.ProcessOverdue(context.Background()); err != nil {
		t.Fatalf("process overdue: %v", err)
	}
	if !called {
		t.Fatal("expected cleanup handler to be called")
	}
}

func TestCleanupWorkerProcessOverdue_ContinuesOnHandlerError(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	due := time.Now().UTC().Add(-time.Minute)
	mustSave := func(project string, service string) {
		t.Helper()
		if err := store.Save(project, DeploymentState{
			Service:   service,
			Strategy:  StrategyBlueGreen,
			Blue:      []string{"a"},
			Green:     []string{"b"},
			Active:    ColorBlue,
			CreatedAt: time.Now().UTC(),
			CleanupAt: &due,
		}); err != nil {
			t.Fatalf("save state: %v", err)
		}
	}
	mustSave("proj-a", "api-a")
	mustSave("proj-b", "api-b")

	called := map[string]bool{}
	worker := NewCleanupWorker(store, func(_ context.Context, project string, _ DeploymentState) error {
		called[project] = true
		if project == "proj-a" {
			return errors.New("boom")
		}
		return nil
	})

	err := worker.ProcessOverdue(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if !called["proj-a"] || !called["proj-b"] {
		t.Fatalf("expected both projects to be processed, got %+v", called)
	}
}

func TestCleanupWorkerProcessOverdue_SkipsInvalidStateFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	due := time.Now().UTC().Add(-time.Minute)
	if err := store.Save("proj-valid", DeploymentState{
		Service:   "api",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"a"},
		Green:     []string{"b"},
		Active:    ColorBlue,
		CreatedAt: time.Now().UTC(),
		CleanupAt: &due,
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proj-bad.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write broken state: %v", err)
	}

	var called bool
	worker := NewCleanupWorker(store, func(_ context.Context, _ string, _ DeploymentState) error {
		called = true
		return nil
	})

	if err := worker.ProcessOverdue(context.Background()); err != nil {
		t.Fatalf("process overdue: %v", err)
	}
	if !called {
		t.Fatal("expected valid state to be processed")
	}
}

func TestCleanupWorkerProcessOverdue_ObserverReportsScheduledAndDue(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	now := time.Now().UTC()
	due := now.Add(-time.Minute)
	later := now.Add(3 * time.Minute)

	if err := store.Save("proj-due", DeploymentState{
		Service:   "api-due",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"a"},
		Green:     []string{"b"},
		Active:    ColorBlue,
		CreatedAt: now,
		CleanupAt: &due,
	}); err != nil {
		t.Fatalf("save due state: %v", err)
	}
	if err := store.Save("proj-later", DeploymentState{
		Service:   "api-later",
		Strategy:  StrategyBlueGreen,
		Blue:      []string{"c"},
		Green:     []string{"d"},
		Active:    ColorBlue,
		CreatedAt: now,
		CleanupAt: &later,
	}); err != nil {
		t.Fatalf("save future state: %v", err)
	}

	seenKinds := map[CleanupObservationKind]int{}
	worker := NewCleanupWorker(store, func(_ context.Context, _ string, _ DeploymentState) error {
		return nil
	}).WithObserver(func(ob CleanupObservation) {
		seenKinds[ob.Kind]++
	})
	worker.now = func() time.Time { return now }

	if err := worker.ProcessOverdue(context.Background()); err != nil {
		t.Fatalf("process overdue: %v", err)
	}

	if seenKinds[CleanupObservationDue] != 1 {
		t.Fatalf("expected one due observation, got %d", seenKinds[CleanupObservationDue])
	}
	if seenKinds[CleanupObservationHandled] != 1 {
		t.Fatalf("expected one handled observation, got %d", seenKinds[CleanupObservationHandled])
	}
	if seenKinds[CleanupObservationScheduled] != 1 {
		t.Fatalf("expected one scheduled observation, got %d", seenKinds[CleanupObservationScheduled])
	}
}
