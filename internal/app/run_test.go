package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/registry"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/state"
)

func TestRegisterCurrentWorkingDir(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	store := registry.NewStore(filepath.Join(base, "registry.json"))
	if err := registerCurrentWorkingDir(store); err != nil {
		t.Fatalf("register current working dir: %v", err)
	}
	entries, err := store.List()
	if err != nil {
		t.Fatalf("list registry: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	canonicalProjectDir, err := registry.CanonicalizeWorkingDir(projectDir)
	if err != nil {
		t.Fatalf("canonicalize project dir: %v", err)
	}
	if entries[0].WorkingDir != canonicalProjectDir {
		t.Fatalf("unexpected working dir: %s", entries[0].WorkingDir)
	}
}

func TestRegistryStrictMode(t *testing.T) {
	prev := os.Getenv(envRegistryStrict)
	defer func() { _ = os.Setenv(envRegistryStrict, prev) }()

	_ = os.Setenv(envRegistryStrict, "true")
	if !registryStrictMode() {
		t.Fatal("expected strict mode for true")
	}

	_ = os.Setenv(envRegistryStrict, "0")
	if registryStrictMode() {
		t.Fatal("expected strict mode disabled for 0")
	}
}

func TestRunAutoCleanupAcrossRegisteredProjects(t *testing.T) {
	base := t.TempDir()
	regPath := filepath.Join(base, "registry.json")
	regStore := registry.NewStore(regPath)

	projectA := filepath.Join(base, "a")
	projectB := filepath.Join(base, "b")
	for _, dir := range []string{projectA, projectB} {
		if err := os.MkdirAll(filepath.Join(dir, ".ztd", "state"), 0o755); err != nil {
			t.Fatalf("mkdir state dir: %v", err)
		}
		if _, err := regStore.Register(dir); err != nil {
			t.Fatalf("register %s: %v", dir, err)
		}
	}

	prevAdapter := os.Getenv("ZTD_COMPOSE_ADAPTER")
	defer func() { _ = os.Setenv("ZTD_COMPOSE_ADAPTER", prevAdapter) }()
	_ = os.Setenv("ZTD_COMPOSE_ADAPTER", "api")

	runner := NewRunner(logrus.New())
	err := runner.runAutoCleanup(context.Background(), cli.Config{
		Action:            cli.ActionAutoRun,
		TraefikConfigFile: cli.DefaultTraefikConfig,
	}, regStore)
	if err != nil {
		t.Fatalf("run auto cleanup: %v", err)
	}
}

func TestEnsureTraefikConfigDirCreatesParentDir(t *testing.T) {
	base := t.TempDir()
	configPath := filepath.Join(base, "traefik", "dynamic_conf.yml")

	if err := ensureTraefikConfigDir(configPath); err != nil {
		t.Fatalf("ensure traefik config dir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "traefik")); err != nil {
		t.Fatalf("expected traefik directory to exist: %v", err)
	}
}

func TestEnsureTraefikConfigDirEmptyPath(t *testing.T) {
	if err := ensureTraefikConfigDir(""); err != nil {
		t.Fatalf("empty config path should be ignored, got: %v", err)
	}
}

func TestEnsureNoConflictingActiveDeployment_BlocksDifferentStrategy(t *testing.T) {
	store := state.NewStore(t.TempDir())
	if err := store.Save("proj--helloworld", state.DeploymentState{
		Service:   "helloworld",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-1"},
		Green:     []string{"green-1"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	err := ensureNoConflictingActiveDeployment(cli.Config{
		Service:  "helloworld",
		Strategy: cli.StrategyRolling,
		Action:   cli.ActionDeploy,
	}, store)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "cannot run rolling deploy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureNoConflictingActiveDeployment_AllowsSameStrategy(t *testing.T) {
	store := state.NewStore(t.TempDir())
	if err := store.Save("proj--helloworld", state.DeploymentState{
		Service:   "helloworld",
		Strategy:  state.StrategyCanary,
		Old:       []string{"old-1"},
		New:       []string{"new-1"},
		Weight:    10,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	err := ensureNoConflictingActiveDeployment(cli.Config{
		Service:  "helloworld",
		Strategy: cli.StrategyCanary,
		Action:   cli.ActionDeploy,
	}, store)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestEnsureNoConflictingActiveDeployment_IgnoresNonDeployAction(t *testing.T) {
	store := state.NewStore(t.TempDir())
	if err := store.Save("proj--helloworld", state.DeploymentState{
		Service:   "helloworld",
		Strategy:  state.StrategyBlueGreen,
		Blue:      []string{"blue-1"},
		Green:     []string{"green-1"},
		Active:    state.ColorBlue,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	err := ensureNoConflictingActiveDeployment(cli.Config{
		Service:  "helloworld",
		Strategy: cli.StrategyCanary,
		Action:   cli.ActionCleanup,
	}, store)
	if err != nil {
		t.Fatalf("expected no error for cleanup action, got: %v", err)
	}
}
