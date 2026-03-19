package state

import (
	"context"
	"time"
)

type CleanupHandler func(ctx context.Context, project string, st DeploymentState) error

type CleanupWorker struct {
	store   *Store
	handler CleanupHandler
	now     func() time.Time
}

func NewCleanupWorker(store *Store, handler CleanupHandler) *CleanupWorker {
	return &CleanupWorker{
		store:   store,
		handler: handler,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (w *CleanupWorker) ProcessOverdue(ctx context.Context) error {
	projects, err := w.store.ListProjects()
	if err != nil {
		return err
	}

	now := w.now()
	for _, project := range projects {
		st, err := w.store.Load(project)
		if err != nil {
			continue
		}
		if st.CleanupAt == nil || st.CleanupAt.After(now) {
			continue
		}
		if err := w.handler(ctx, project, st); err != nil {
			return err
		}
	}
	return nil
}
