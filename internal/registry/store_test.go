package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRegisterAndListDeduplicatesByWorkingDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	regPath := filepath.Join(base, "projects.json")
	store := NewStore(regPath)

	projectDir := filepath.Join(base, "project-a")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	if _, err := store.Register(projectDir); err != nil {
		t.Fatalf("register 1: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := store.Register(projectDir); err != nil {
		t.Fatalf("register 2: %v", err)
	}

	entries, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	canonicalProjectDir, err := CanonicalizeWorkingDir(projectDir)
	if err != nil {
		t.Fatalf("canonicalize project dir: %v", err)
	}
	if entries[0].WorkingDir != canonicalProjectDir {
		t.Fatalf("unexpected working dir: %s", entries[0].WorkingDir)
	}
	if entries[0].LastSeenAt.Before(entries[0].RegisteredAt) {
		t.Fatal("expected lastSeenAt to be >= registeredAt")
	}
}

func TestStoreSetAndClearLastError(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	regPath := filepath.Join(base, "projects.json")
	store := NewStore(regPath)

	projectDir := filepath.Join(base, "project-a")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if _, err := store.Register(projectDir); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := store.SetLastError(projectDir, errors.New("disk unavailable")); err != nil {
		t.Fatalf("set last error: %v", err)
	}
	entries, err := store.List()
	if err != nil {
		t.Fatalf("list after set: %v", err)
	}
	if entries[0].LastError == "" || entries[0].LastErrorAt == nil {
		t.Fatalf("expected error fields to be set, got %+v", entries[0])
	}

	if err := store.ClearLastError(projectDir); err != nil {
		t.Fatalf("clear last error: %v", err)
	}
	entries, err = store.List()
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if entries[0].LastError != "" || entries[0].LastErrorAt != nil {
		t.Fatalf("expected error fields to be cleared, got %+v", entries[0])
	}
}

func TestCanonicalizeWorkingDirRequiresExistingDirectory(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	filePath := filepath.Join(base, "not-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := CanonicalizeWorkingDir(filePath); err == nil {
		t.Fatal("expected canonicalize error for non-directory")
	}
}

func TestNewStoreDefaultsToHomeRegistryPath(t *testing.T) {
	t.Parallel()

	prevEnv := os.Getenv(EnvRegistryPath)
	defer func() { _ = os.Setenv(EnvRegistryPath, prevEnv) }()
	_ = os.Unsetenv(EnvRegistryPath)

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		t.Skip("home directory is not available")
	}

	store := NewStore("")
	want := filepath.Join(home, filepath.FromSlash(DefaultRegistryRelativePath))
	if store.Path() != want {
		t.Fatalf("expected default path %s, got %s", want, store.Path())
	}
}

func TestNewStoreExpandsHomeInEnvPath(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		t.Skip("home directory is not available")
	}

	prevEnv := os.Getenv(EnvRegistryPath)
	defer func() { _ = os.Setenv(EnvRegistryPath, prevEnv) }()
	_ = os.Setenv(EnvRegistryPath, "~/.ztd/custom/projects.json")

	store := NewStore("")
	want := filepath.Join(home, ".ztd", "custom", "projects.json")
	if store.Path() != want {
		t.Fatalf("expected env-expanded path %s, got %s", want, store.Path())
	}
}
