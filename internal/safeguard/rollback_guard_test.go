package safeguard

import (
	"context"
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestRollbackGuard_RunOnError(t *testing.T) {
	t.Parallel()

	called := false
	guard := NewRollbackGuard(logrus.New(), "test", func(context.Context) error {
		called = true
		return nil
	})

	opErr := errors.New("boom")
	guard.Run(context.Background(), &opErr)
	if !called {
		t.Fatal("expected cleanup to be called")
	}
}

func TestRollbackGuard_Disarm(t *testing.T) {
	t.Parallel()

	called := false
	guard := NewRollbackGuard(logrus.New(), "test", func(context.Context) error {
		called = true
		return nil
	})
	guard.Disarm()

	opErr := errors.New("boom")
	guard.Run(context.Background(), &opErr)
	if called {
		t.Fatal("expected cleanup not to be called when disarmed")
	}
}
