package state

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type CleanupHandler func(ctx context.Context, project string, st DeploymentState) error
type CleanupObservationKind string

const (
	CleanupObservationStateLoadError CleanupObservationKind = "state-load-error"
	CleanupObservationNoSchedule     CleanupObservationKind = "no-schedule"
	CleanupObservationScheduled      CleanupObservationKind = "scheduled"
	CleanupObservationDue            CleanupObservationKind = "due"
	CleanupObservationHandled        CleanupObservationKind = "handled"
	CleanupObservationFailed         CleanupObservationKind = "failed"
)

type CleanupObservation struct {
	Kind    CleanupObservationKind
	Now     time.Time
	Project string
	State   DeploymentState
	Err     error
}

type CleanupObserver func(observation CleanupObservation)

type CleanupWorker struct {
	store    *Store
	handler  CleanupHandler
	now      func() time.Time
	observer CleanupObserver
}

func NewCleanupWorker(store *Store, handler CleanupHandler) *CleanupWorker {
	return &CleanupWorker{
		store:   store,
		handler: handler,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (w *CleanupWorker) WithObserver(observer CleanupObserver) *CleanupWorker {
	w.observer = observer
	return w
}

func (w *CleanupWorker) ProcessOverdue(ctx context.Context) error {
	projects, err := w.store.ListProjects()
	if err != nil {
		return err
	}

	now := w.now()
	var processErrs []error
	for _, project := range projects {
		st, err := w.store.Load(project)
		if err != nil {
			w.observe(CleanupObservation{
				Kind:    CleanupObservationStateLoadError,
				Now:     now,
				Project: project,
				Err:     err,
			})
			continue
		}
		if st.CleanupAt == nil {
			w.observe(CleanupObservation{
				Kind:    CleanupObservationNoSchedule,
				Now:     now,
				Project: project,
				State:   st,
			})
			continue
		}
		if st.CleanupAt.After(now) {
			w.observe(CleanupObservation{
				Kind:    CleanupObservationScheduled,
				Now:     now,
				Project: project,
				State:   st,
			})
			continue
		}
		w.observe(CleanupObservation{
			Kind:    CleanupObservationDue,
			Now:     now,
			Project: project,
			State:   st,
		})
		if err := w.handler(ctx, project, st); err != nil {
			w.observe(CleanupObservation{
				Kind:    CleanupObservationFailed,
				Now:     now,
				Project: project,
				State:   st,
				Err:     err,
			})
			processErrs = append(processErrs, fmt.Errorf("cleanup failed for project %s: %w", project, err))
			continue
		}
		w.observe(CleanupObservation{
			Kind:    CleanupObservationHandled,
			Now:     now,
			Project: project,
			State:   st,
		})
	}
	return errors.Join(processErrs...)
}

func (w *CleanupWorker) observe(observation CleanupObservation) {
	if w.observer == nil {
		return
	}
	w.observer(observation)
}
