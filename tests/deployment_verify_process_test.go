package tests

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/deployments"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"go.yaml.in/yaml/v3"
)

type deploymentCommitBarrier struct {
	river.MiddlewareDefaults
	path string
}

func (m *deploymentCommitBarrier) Work(ctx context.Context, _ *rivertype.JobRow, next func(context.Context) error) error {
	if err := next(ctx); err != nil {
		return err
	}
	if err := os.WriteFile(m.path, []byte("committed"), 0o600); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestDeploymentVerificationProcess(t *testing.T) {
	mode := os.Getenv("VERIFY_BE_DEPLOYMENT_PROCESS")
	if mode == "" {
		return
	}
	requireDeploymentVerification(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(t.Context(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	storageClient, err := storage.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := storageClient.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	store := deployments.NewStore(database).WithEventPayloadStorage(objects)
	workers := river.NewWorkers()
	deployments.RegisterWorkers(workers, store)
	options := &river.Config{
		Schema: "public", Workers: workers, Logger: logger,
		Queues:     map[string]river.QueueConfig{deploymentjobs.Queue: {MaxWorkers: 10}},
		JobTimeout: 10 * time.Second, RescueStuckJobsAfter: 15 * time.Second,
		DurablePeriodicJobs: river.DurablePeriodicJobsConfig{Enabled: true},
	}
	if mode == "after_commit" {
		options.Middleware = []rivertype.Middleware{&deploymentCommitBarrier{path: os.Getenv("VERIFY_BE_DEPLOYMENT_COMMITTED")}}
	}
	client, err := river.NewClient(riverdatabasesql.NewWithPgxListener(database.SQLDB(), database.ListenerPool()), options)
	if err != nil {
		t.Fatal(err)
	}
	store.Configure(client)
	ctx, cancel := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("VERIFY_BE_DEPLOYMENT_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		t.Error(err)
	}
}

type deploymentVerifyProcess struct {
	command   *exec.Cmd
	done      chan error
	finished  bool
	committed string
}

func (f *deploymentVerification) process(t *testing.T, mode string) *deploymentVerifyProcess {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	data, err := yaml.Marshal(f.app.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(directory, "ready")
	committed := filepath.Join(directory, "committed")
	command := exec.Command(os.Args[0], "-test.run=^TestDeploymentVerificationProcess$", "-test.timeout=3m")
	command.Env = append(os.Environ(), "CONFIG_FILE="+configPath, "VERIFY_BE_DEPLOYMENT_PROCESS="+mode, "VERIFY_BE_DEPLOYMENT_READY="+ready, "VERIFY_BE_DEPLOYMENT_COMMITTED="+committed)
	output, err := os.OpenFile(filepath.Join(directory, "process.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	})
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	p := &deploymentVerifyProcess{command: command, done: make(chan error, 1), committed: committed}
	go func() { p.done <- command.Wait() }()
	t.Cleanup(func() { p.stop(t, false) })
	deploymentWait(t, "River process startup", 10*time.Second, func() bool {
		select {
		case err := <-p.done:
			p.finished = true
			t.Fatalf("River process exited before readiness: %v", err)
		default:
		}
		_, err := os.Stat(ready)
		return err == nil
	})
	return p
}

func (p *deploymentVerifyProcess) stop(t *testing.T, kill bool) {
	t.Helper()
	if p.finished {
		return
	}
	var err error
	if kill {
		err = p.command.Process.Kill()
	} else {
		err = p.command.Process.Signal(syscall.SIGTERM)
	}
	if err != nil {
		t.Error(err)
	}
	select {
	case err := <-p.done:
		p.finished = true
		if !kill && err != nil {
			t.Errorf("River process failed: %v", err)
		}
		if kill && err == nil {
			t.Error("expected killed process to fail")
		}
	case <-time.After(10 * time.Second):
		_ = p.command.Process.Kill()
		<-p.done
		p.finished = true
		t.Error("River process failed to stop")
	}
}
