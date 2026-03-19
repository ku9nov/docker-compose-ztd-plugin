package canary

import (
	"context"

	"github.com/sirupsen/logrus"
)

type Options struct {
	Service      string
	ComposeFiles []string
	EnvFiles     []string
	Weight       int
}

func Run(_ context.Context, log *logrus.Logger, opt Options) error {
	log.WithFields(logrus.Fields{
		"service": opt.Service,
		"weight":  opt.Weight,
	}).Info("==> Canary deployment strategy selected (stub): strategy logic is not implemented yet")
	return nil
}
