package state

import (
	"path/filepath"
	"testing"
)

func TestTryExclusiveFileLock(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "auto-cleanup.lock")

	unlock, acquired, err := TryExclusiveFileLock(lockPath)
	if err != nil {
		t.Fatalf("unexpected lock error: %v", err)
	}
	if !acquired {
		t.Fatal("expected first lock to be acquired")
	}
	defer func() {
		if err := unlock(); err != nil {
			t.Fatalf("unlock failed: %v", err)
		}
	}()

	unlockSecond, acquiredSecond, err := TryExclusiveFileLock(lockPath)
	if err != nil {
		t.Fatalf("unexpected second lock error: %v", err)
	}
	if acquiredSecond {
		defer func() { _ = unlockSecond() }()
		t.Fatal("expected second lock to be rejected")
	}
	if unlockSecond != nil {
		t.Fatal("expected nil unlock function when lock is not acquired")
	}
}
