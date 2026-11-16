package environments

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

type resumeTestStore struct {
	target   db.ManagerResumeTarget
	missing  bool
	err      error
	phaseErr error
	lock     sync.Mutex
	events   []string
}

func (s *resumeTestStore) FindManagerResumeTarget(context.Context, string, string) (db.ManagerResumeTarget, bool, error) {
	return s.target, !s.missing, s.err
}

func (s *resumeTestStore) WithManagerResumeLock(_ context.Context, _ string, resume func() error) error {
	if !s.lock.TryLock() {
		return db.ErrPreconditionFailed
	}
	defer s.lock.Unlock()
	return resume()
}

func (s *resumeTestStore) SetManagerResumePhase(_ context.Context, _ db.ManagerResumeTarget, phase string) error {
	s.events = append(s.events, phase)
	if s.phaseErr != nil {
		return s.phaseErr
	}
	s.target.Phase = phase
	return nil
}

type resumeTestProvider struct {
	state     string
	workspace string
	onRenew   func()
	stateErr  error
	resumeErr error
	events    *[]string
}

func (p *resumeTestProvider) GetStatus(context.Context, string) (e2bruntime.SandboxStatus, error) {
	*p.events = append(*p.events, "state")
	return e2bruntime.SandboxStatus{State: p.state, WorkspaceUUID: p.workspace}, p.stateErr
}

func (p *resumeTestProvider) RenewTimeout(context.Context, string, time.Duration) error {
	*p.events = append(*p.events, "renew")
	if p.onRenew != nil {
		p.onRenew()
	}
	return nil
}

func (p *resumeTestProvider) SetTimeout(context.Context, string, time.Duration) error {
	*p.events = append(*p.events, "resume")
	return p.resumeErr
}

func newResumeControllerTest() (*managerResumeController, *resumeTestStore, *resumeTestProvider) {
	store := &resumeTestStore{target: db.ManagerResumeTarget{SandboxUUID: "sandbox", WorkspaceUUID: "workspace", Transport: "claim"}}
	provider := &resumeTestProvider{state: "paused", workspace: "workspace", events: &store.events}
	controller := &managerResumeController{store: store, provider: provider, required: "0.1.1", slots: make(chan struct{}, 2), upgrade: func(context.Context, string, db.ManagerResumeTarget) error {
		store.events = append(store.events, "upgrade")
		return nil
	}}
	return controller, store, provider
}

func TestManagerResumeRejectsMissingScopeAndInvalidState(t *testing.T) {
	for _, test := range []struct {
		state, workspace string
		requests         int
	}{
		{"paused", "", 1}, {"unknown", "workspace", 2}, {"paused", "foreign-workspace", 2},
	} {
		c, s, p := newResumeControllerTest()
		p.state, p.workspace = test.state, test.workspace
		if err := c.resume(t.Context(), "provider", time.Minute); err == nil {
			t.Fatal("invalid provider state accepted")
		}
		if len(s.events) != test.requests || s.target.Phase != "" {
			t.Fatalf("events=%v phase=%s", s.events, s.target.Phase)
		}
	}
}

func TestManagerResumeCapacityDoesNotBlockRunningSandbox(t *testing.T) {
	c, s, p := newResumeControllerTest()
	c.slots <- struct{}{}
	c.slots <- struct{}{}
	if err := c.resume(t.Context(), "provider", time.Minute); !errors.Is(err, db.ErrPreconditionFailed) {
		t.Fatalf("capacity error=%v", err)
	}
	if s.target.Phase != "" {
		t.Fatal("capacity failure changed resume state")
	}
	p.state = "running"
	if err := c.resume(t.Context(), "provider", time.Minute); err != nil {
		t.Fatalf("running sandbox blocked: %v", err)
	}
}

func TestManagerResumeFailuresKeepClaimsBlocked(t *testing.T) {
	failure := errors.New("failure")
	for _, test := range []struct {
		name   string
		setup  func(*managerResumeController, *resumeTestStore, *resumeTestProvider)
		phase  string
		events []string
	}{
		{"lookup", func(_ *managerResumeController, s *resumeTestStore, _ *resumeTestProvider) { s.err = failure }, "", []string{"state"}},
		{"state", func(_ *managerResumeController, _ *resumeTestStore, p *resumeTestProvider) { p.stateErr = failure }, "", []string{"state"}},
		{"gate", func(_ *managerResumeController, s *resumeTestStore, _ *resumeTestProvider) { s.phaseErr = failure }, "", []string{"state", "state", "checking"}},
		{"resume", func(_ *managerResumeController, _ *resumeTestStore, p *resumeTestProvider) { p.resumeErr = failure }, "checking", []string{"state", "state", "checking", "resume"}},
		{"upgrade", func(c *managerResumeController, _ *resumeTestStore, _ *resumeTestProvider) {
			c.upgrade = func(context.Context, string, db.ManagerResumeTarget) error { return failure }
		}, "checking", []string{"state", "state", "checking", "resume"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, s, p := newResumeControllerTest()
			test.setup(c, s, p)
			if err := c.resume(t.Context(), "provider", time.Minute); !errors.Is(err, failure) {
				t.Fatalf("error=%v", err)
			}
			if s.target.Phase != test.phase || !reflect.DeepEqual(s.events, test.events) {
				t.Fatalf("phase=%s events=%v", s.target.Phase, s.events)
			}
		})
	}
}

func TestManagerResumeLegacyRequiresKnownSufficientVersion(t *testing.T) {
	for _, version := range []string{"", "0.1.0", "0.1.1-rc.1"} {
		t.Run(version, func(t *testing.T) {
			c, s, _ := newResumeControllerTest()
			s.target.Transport = "legacy"
			s.target.Version = version
			if err := c.resume(t.Context(), "provider", time.Minute); err == nil {
				t.Fatal("legacy upgrade accepted")
			}
			if !reflect.DeepEqual(s.events, []string{"state", "state"}) {
				t.Fatalf("woke old worker: %v", s.events)
			}
		})
	}
	for _, version := range []string{"0.1.1", "0.2.0"} {
		c, s, _ := newResumeControllerTest()
		s.target.Transport = "legacy"
		s.target.Version = version
		if err := c.resume(t.Context(), "provider", time.Minute); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.events, []string{"state", "state", "resume"}) {
			t.Fatalf("events=%v", s.events)
		}
	}
}

func TestManagerResumeSerializesAcrossControllers(t *testing.T) {
	c, s, _ := newResumeControllerTest()
	s.lock.Lock()
	err := c.resume(t.Context(), "provider", time.Minute)
	s.lock.Unlock()
	if !errors.Is(err, db.ErrPreconditionFailed) || len(s.events) != 1 {
		t.Fatalf("error=%v events=%v", err, s.events)
	}
}

func TestManagerResumeDisabledVersionDoesNotAbandonPendingUpgrade(t *testing.T) {
	c, s, _ := newResumeControllerTest()
	c.required = ""
	s.target.Phase = "starting"
	if err := c.resume(t.Context(), "provider", time.Minute); err == nil || len(s.events) != 1 {
		t.Fatalf("pending upgrade bypassed: %v %v", err, s.events)
	}
	s.target.Phase = ""
	s.events = nil
	if err := c.resume(t.Context(), "provider", time.Minute); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.events, []string{"state", "resume"}) {
		t.Fatalf("events=%v", s.events)
	}
}

func TestManagerResumeOnlyUpgradesPausedOrInterruptedResumes(t *testing.T) {
	for _, test := range []struct {
		name, state, phase string
		missing            bool
		events             []string
	}{
		{"running", "running", "", false, []string{"state", "state", "renew", "state"}},
		{"unmanaged", "paused", "", true, []string{"state", "resume"}},
		{"paused", "paused", "", false, []string{"state", "state", "checking", "resume", "upgrade", "resume"}},
		{"retry", "running", "starting", false, []string{"state", "state", "resume", "upgrade", "resume"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, s, p := newResumeControllerTest()
			p.state = test.state
			s.target.Phase = test.phase
			s.missing = test.missing
			if err := c.resume(t.Context(), "provider", time.Minute); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.events, test.events) {
				t.Fatalf("events=%v", s.events)
			}
		})
	}
}

func TestManagerResumeHandlesPauseDuringRunningRenewal(t *testing.T) {
	c, s, p := newResumeControllerTest()
	p.state = "running"
	p.onRenew = func() { p.state = "paused" }
	if err := c.resume(t.Context(), "provider", time.Minute); err != nil {
		t.Fatal(err)
	}
	want := []string{"state", "state", "renew", "state", "checking", "resume", "upgrade", "resume"}
	if !reflect.DeepEqual(s.events, want) {
		t.Fatalf("pause race bypassed upgrade: %v", s.events)
	}
}
