package codesessions

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type blockingSandboxProvisioner struct {
	calls    atomic.Int32
	release  chan struct{}
	finished chan error
}

func (p *blockingSandboxProvisioner) EnsureSessionSandbox(ctx context.Context, _ db.CodeSession) (db.EnvironmentSandbox, error) {
	p.calls.Add(1)
	select {
	case <-p.release:
		p.finished <- nil
		return db.EnvironmentSandbox{}, nil
	case <-ctx.Done():
		p.finished <- ctx.Err()
		return db.EnvironmentSandbox{}, ctx.Err()
	}
}

func TestEnsureSessionSandboxReportsProvisioningFailure(t *testing.T) {
	failure := errors.New("provider unavailable")
	handler := &Handler{service: &Service{sandboxProvisioner: failingSandboxProvisioner{failure}}}
	err := handler.ensureSessionSandbox(context.Background(), db.CodeSession{UUID: "22222222-2222-4222-8222-222222222222"}, false)
	if !errors.Is(err, failure) || !errors.Is(err, errSandboxUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsureSessionSandboxSurvivesCallerCancellation(t *testing.T) {
	provisioner := &blockingSandboxProvisioner{release: make(chan struct{}), finished: make(chan error, 1)}
	handler := &Handler{service: &Service{sandboxProvisioner: provisioner}}
	session := db.CodeSession{UUID: "11111111-1111-4111-8111-111111111111"}

	first, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- handler.ensureSessionSandbox(first, session, false) }()
	for provisioner.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) || !errors.Is(err, errSandboxUnavailable) {
		t.Fatalf("cancelled caller error = %v", err)
	}
	select {
	case err := <-provisioner.finished:
		t.Fatalf("provisioning stopped with the caller: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	retry := make(chan error, 1)
	go func() { retry <- handler.ensureSessionSandbox(context.Background(), session, false) }()
	time.Sleep(20 * time.Millisecond)
	close(provisioner.release)
	if err := <-retry; err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if err := <-provisioner.finished; err != nil {
		t.Fatalf("provisioning error = %v", err)
	}
	if calls := provisioner.calls.Load(); calls != 1 {
		t.Fatalf("provisioning calls = %d, want 1", calls)
	}
}

type failingSandboxProvisioner struct{ err error }

func (p failingSandboxProvisioner) EnsureSessionSandbox(context.Context, db.CodeSession) (db.EnvironmentSandbox, error) {
	return db.EnvironmentSandbox{}, p.err
}
