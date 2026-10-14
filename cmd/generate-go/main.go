package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cached := len(os.Args) == 2 && os.Args[1] == "--cached"
	if len(os.Args) > 1 && !cached {
		return errors.New("usage: generate-go.sh [--cached]")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return generate(ctx, root, cached)
}

func acquireLock(ctx context.Context, directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func generate(ctx context.Context, root string, cached bool) error {
	directory := filepath.Join(root, "tmp", "go-generation")
	lock, err := acquireLock(ctx, directory)
	if err != nil {
		return err
	}
	defer lock.Close()
	cachePath := filepath.Join(directory, "cache.json")
	before, inputErr := inputDigest(ctx, root)
	if cached && inputErr == nil && cacheMatches(root, cachePath, before) {
		fmt.Println("Go generation cache hit; skipping generation")
		return nil
	}
	if err := removeCache(cachePath); err != nil {
		return err
	}
	if err := removeOutputs(root); err != nil {
		return err
	}
	fmt.Println("Generating Go DB mappers")
	command := goCommand(ctx, root, "generate", "./internal/db")
	command.ExtraFiles = []*os.File{lock}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("generate DB mappers: %w", err)
	}
	after, err := inputDigest(ctx, root)
	if inputErr != nil || err != nil || before != after {
		fmt.Println("Go generation cache unavailable; next invocation will regenerate")
		return nil
	}
	outputs, err := outputDigest(root)
	if err != nil {
		return err
	}
	return saveCache(directory, cachePath, generationCache{Inputs: after, Outputs: outputs})
}

func goCommand(ctx context.Context, root string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = root
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	return command
}
