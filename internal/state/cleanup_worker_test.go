package state

import (
	"context"
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
