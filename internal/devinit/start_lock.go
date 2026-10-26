package devinit

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func StartLocked(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return startLocked(ctx, filepath.Join(cache, "oma", "dev-init", "backend-start"), args, stdout, stderr)
}

func startLocked(ctx context.Context, directory string, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("start-locked requires a command")
	}
	unlock, err := acquireInitLock(directory)
	if err != nil {
		return err
	}
	defer func() { unlock() }()
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, stdout, stderr
	command.ExtraFiles = []*os.File{writer}
	command.Env = append(os.Environ(), "OMA_START_LOCK_FD=3")
	if err := command.Start(); err != nil {
		return err
	}
	_ = writer.Close()
	ready := make(chan struct{})
	go func() {
		if _, err := io.CopyN(io.Discard, reader, 1); err == nil {
			close(ready)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ready:
		unlock()
		unlock = func() {}
		return <-done
	}
}
