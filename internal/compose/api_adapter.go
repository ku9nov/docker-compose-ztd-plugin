package compose

import (
	"context"
	"fmt"
)

// APIAdapter is a placeholder for a future Docker Compose API-based integration.
//
// Ideally, the project would interact with Docker Compose directly via API,
// without relying on shell commands. However, the current approach using the
// shell adapter remains more stable and ensures consistent behavior across
// plugin flows and environments.
//
// This adapter exists as a forward-looking stub, to be implemented once
// the API-based approach becomes sufficiently stable and provides clear
// practical benefits over the existing solution.

type APIAdapter struct{}

func NewAPIAdapter() *APIAdapter {
	return &APIAdapter{}
}

func (a *APIAdapter) Up(_ context.Context, _ []string, _ []string, _ string, _ bool, _ bool) error {
	return fmt.Errorf("compose API adapter is not enabled yet")
}

func (a *APIAdapter) Scale(_ context.Context, _ []string, _ []string, _ string, _ int) error {
	return fmt.Errorf("compose API adapter is not enabled yet")
}

func (a *APIAdapter) PsQuiet(_ context.Context, _ []string, _ []string, _ string) ([]string, error) {
	return nil, fmt.Errorf("compose API adapter is not enabled yet")
}

func (a *APIAdapter) LogsFollowTail(_ context.Context, _ []string, _ string, _ int) error {
	return fmt.Errorf("compose API adapter is not enabled yet")
}
