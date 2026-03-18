package safeguard

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

type CleanupFunc func(ctx context.Context) error

// RollbackGuard runs a compensating cleanup action if the parent operation fails.
type RollbackGuard struct {
	log     *logrus.Logger
	name    string
	cleanup CleanupFunc
	armed   bool
}

func NewRollbackGuard(log *logrus.Logger, name string, cleanup CleanupFunc) *RollbackGuard {
	return &RollbackGuard{
		log:     log,
		name:    name,
		cleanup: cleanup,
		armed:   true,
	}
}

func (g *RollbackGuard) Disarm() {
	g.armed = false
}

func (g *RollbackGuard) Run(ctx context.Context, opErr *error) {
	if !g.armed || opErr == nil || *opErr == nil {
		return
	}

	g.log.Warnf("==> %s guard activated. Running cleanup.", g.name)
	if err := g.cleanup(ctx); err != nil {
		g.log.Warnf("==> %s guard cleanup failed: %v", g.name, err)
	}
}

func WrapErrors(primary string, errs ...error) error {
	var first error
	for _, err := range errs {
		if err != nil {
			first = err
			break
		}
	}
	if first == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", primary, first)
}
