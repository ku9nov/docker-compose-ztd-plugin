package healthdiag

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"
)

type Docker interface {
	HealthStatus(ctx context.Context, containerID string) (string, error)
	LogsTail(ctx context.Context, containerID string, tail int) (string, error)
}

func LogUnhealthyContainerLogs(ctx context.Context, log *logrus.Logger, docker Docker, containerIDs []string, tail int) {
	for _, id := range containerIDs {
		status, err := docker.HealthStatus(ctx, id)
		if err != nil {
			log.Warnf("==> Unable to read health status for container %s: %v", id, err)
			continue
		}
		if status == "healthy" {
			continue
		}

		logs, err := docker.LogsTail(ctx, id, tail)
		if err != nil {
			log.Warnf("==> Unable to read last %d log lines for container %s: %v", tail, id, err)
			continue
		}

		logs = strings.TrimSpace(logs)
		if logs == "" {
			log.Warnf("==> Container %s is %s. Last %d log lines are empty.", id, status, tail)
			continue
		}

		log.Errorf("==> Container %s is %s. Last %d log lines:\n%s", id, status, tail, logs)
	}
}
