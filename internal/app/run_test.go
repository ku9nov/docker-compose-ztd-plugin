package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/registry"
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
