package bluegreen

import (
	"context"

	"github.com/sirupsen/logrus"
)

type Options struct {
	Service      string
	ComposeFiles []string
	EnvFiles     []string
	HostMode     string
	HeadersMode  string
	CookiesMode  string
	IPMode       string
}

func Run(_ context.Context, log *logrus.Logger, opt Options) error {
	log.WithFields(logrus.Fields{
		"service":      opt.Service,
		"host_mode":    opt.HostMode,
		"headers_mode": opt.HeadersMode,
		"cookies_mode": opt.CookiesMode,
		"ip_mode":      opt.IPMode,
	}).Info("==> Blue-green deployment strategy selected (stub): strategy logic is not implemented yet")
	return nil
}
