package compose

import "context"

type Adapter interface {
	Up(ctx context.Context, files []string, envFiles []string, service string, detached bool, noRecreate bool) error
	Scale(ctx context.Context, files []string, envFiles []string, service string, replicas int) error
	PsQuiet(ctx context.Context, files []string, envFiles []string, service string) ([]string, error)
	LogsFollowTail(ctx context.Context, files []string, service string, tail int) error
}
