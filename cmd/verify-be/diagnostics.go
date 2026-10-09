package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func (e *environment) backendSource(ctx context.Context) (string, error) {
	if e.backendRef == "" {
		return e.root, nil
	}
	commit, err := capture(ctx, e.root, "git", "rev-parse", "--verify", "--end-of-options", e.backendRef+"^{commit}")
	if err != nil {
		return "", errors.New("backend-ref must resolve to a local Git commit")
	}
	e.backendCommit = commit
	source := filepath.Join(e.directory, "backend-source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		return "", err
	}
	archive := filepath.Join(e.directory, "backend.tar")
	if err := e.logged(ctx, "build.log", time.Minute, "git", "archive", "--format=tar", "--output", archive, commit); err != nil {
		return "", err
	}
	if err := e.logged(ctx, "build.log", time.Minute, "tar", "-xf", archive, "-C", source); err != nil {
		return "", err
	}
	return source, nil
}

func (e *environment) captureDiagnostics(ctx context.Context) func() error {
	if !e.diagnostics {
		return func() error { return nil }
	}
	profileCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		if err := e.waitProfileReady(profileCtx); err != nil {
			done <- err
			return
		}
		var cpuErr, traceErr error
		var group sync.WaitGroup
		group.Go(func() { cpuErr = e.downloadProfile(ctx, "cpu.pprof", "profile?seconds=5") })
		group.Go(func() { traceErr = e.downloadProfile(ctx, "runtime.trace", "trace?seconds=2") })
		group.Wait()
		done <- errors.Join(cpuErr, traceErr)
	}()
	return func() error {
		cancel()
		if err := <-done; err != nil {
			return err
		}
		for _, profile := range []struct{ name, path string }{{"heap.pprof", "heap?gc=1"}, {"goroutine.pprof", "goroutine"}} {
			if err := e.downloadProfile(ctx, profile.name, profile.path); err != nil {
				return err
			}
		}
		return e.logged(ctx, "cpu-top.txt", 30*time.Second, "go", "tool", "pprof", "-top", filepath.Join(e.directory, "server"), filepath.Join(e.directory, "cpu.pprof"))
	}
}

func (e *environment) waitProfileReady(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(filepath.Join(e.directory, "profile.ready")); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.New("performance workload did not reach profile readiness")
		case <-ticker.C:
		}
	}
}
func (e *environment) downloadProfile(ctx context.Context, name, path string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+e.diagnosticsAddr+"/debug/pprof/"+path, nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("collect %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("collect %s: HTTP %d", name, response.StatusCode)
	}
	file, err := os.OpenFile(filepath.Join(e.directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(file, io.LimitReader(response.Body, (64<<20)+1))
	if n > 64<<20 {
		copyErr = errors.New("diagnostic profile exceeds 64 MiB limit")
	}
	if n == 0 {
		copyErr = errors.Join(copyErr, errors.New("diagnostic profile is empty"))
	}
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return errors.Join(err, os.Remove(filepath.Join(e.directory, name)))
	}
	return nil
}
