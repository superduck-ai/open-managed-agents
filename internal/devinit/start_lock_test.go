package devinit

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStartupLockIsReleasedAfterListenerSignal(t *testing.T) {
	dir := t.TempDir()
	finish := filepath.Join(dir, "finish")
	entered := filepath.Join(dir, "entered")
	release := filepath.Join(dir, "release")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() {
		first <- startLocked(ctx, dir, []string{"sh", "-c", `touch "$1"; while ! test -f "$3"; do sleep 0.01; done; printf . >&3; while ! test -f "$2"; do sleep 0.01; done`, "sh", entered, finish, release}, io.Discard, io.Discard)
	}()
	defer func() { _ = os.WriteFile(finish, nil, 0o600); <-first }()
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("first startup did not acquire lock")
		case <-time.After(time.Millisecond):
		}
	}
	lock, err := os.OpenFile(filepath.Join(dir, "init.lock"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		t.Fatalf("startup lock was not held before readiness: %v", err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := startLocked(ctx, dir, []string{"sh", "-c", "printf . >&3"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finish); !os.IsNotExist(err) {
		t.Fatal("first service exited before the second acquired the lock")
	}
}

func TestStartupFailureReleasesLock(t *testing.T) {
	dir := t.TempDir()
	if err := startLocked(t.Context(), dir, []string{"sh", "-c", "exit 7"}, io.Discard, io.Discard); err == nil {
		t.Fatal("startup failure was hidden")
	}
	if err := startLocked(t.Context(), dir, []string{"sh", "-c", "printf . >&3"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}
