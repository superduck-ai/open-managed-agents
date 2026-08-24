package vaults

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryOAuthRefreshLeaseHoldCanceledWhileWaiting(t *testing.T) {
	t.Parallel()

	lease := newMemoryOAuthRefreshLease()
	first, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatalf("Hold() error = %v", err)
	}
	defer func() { _ = first() }()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	_, err = lease.Hold(ctx, "vcrd_a")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hold() error = %v, want context deadline", err)
	}
}

func TestMemoryOAuthRefreshLeaseSerializesSameCredential(t *testing.T) {
	t.Parallel()

	lease := newMemoryOAuthRefreshLease()
	var holding atomic.Bool
	var overlapped atomic.Bool

	release, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatalf("Hold() error = %v", err)
	}
	holding.Store(true)

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		second, holdErr := lease.Hold(t.Context(), "vcrd_a")
		if holdErr != nil {
			t.Errorf("second Hold() error = %v", holdErr)
			close(done)
			return
		}
		if holding.Load() {
			overlapped.Store(true)
		}
		second()
		close(done)
	}()
	<-started
	time.Sleep(20 * time.Millisecond)
	holding.Store(false)
	release()
	<-done

	if overlapped.Load() {
		t.Fatal("second Hold ran while the first lease was still held")
	}
}

func TestMemoryOAuthRefreshLeaseCancelDoesNotPinCredential(t *testing.T) {
	t.Parallel()

	lease := newMemoryOAuthRefreshLease()
	const credentialID = "vcrd_race"
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
			defer cancel()
			release, err := lease.Hold(ctx, credentialID)
			if err != nil {
				return
			}
			_ = release()
		}()
	}
	wg.Wait()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	release, err := lease.Hold(ctx, credentialID)
	if err != nil {
		t.Fatalf("Hold after cancel storm: %v (credential likely pinned)", err)
	}
	_ = release()
}

func TestMemoryOAuthRefreshLeaseAllowsDifferentCredentials(t *testing.T) {
	t.Parallel()

	lease := newMemoryOAuthRefreshLease()
	first, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatalf("Hold A error = %v", err)
	}
	defer func() { _ = first() }()
	second, err := lease.Hold(t.Context(), "vcrd_b")
	if err != nil {
		t.Fatalf("Hold B error = %v", err)
	}
	_ = second()
}

func TestNewRedisOAuthRefreshLeaseRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := NewRedisOAuthRefreshLease(nil)
	if !errors.Is(err, errOAuthRefreshLeaseRedisRequired) {
		t.Fatalf("error = %v, want errOAuthRefreshLeaseRedisRequired", err)
	}
}

func TestRedisOAuthRefreshLeaseHoldCanceled(t *testing.T) {
	t.Parallel()

	store := newFakeOAuthRefreshNXStore()
	lease := newRedisOAuthRefreshLease(store, time.Second, 20*time.Millisecond)
	first, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatalf("Hold() error = %v", err)
	}
	defer first()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	_, err = lease.Hold(ctx, "vcrd_a")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hold() error = %v, want context deadline", err)
	}
}

func TestRedisOAuthRefreshLeaseWaitsThenAcquiresAfterRelease(t *testing.T) {
	t.Parallel()

	store := newFakeOAuthRefreshNXStore()
	lease := newRedisOAuthRefreshLease(store, time.Second, time.Millisecond)

	first, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatalf("Hold() error = %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		second, holdErr := lease.Hold(t.Context(), "vcrd_a")
		if holdErr != nil {
			t.Errorf("second Hold() error = %v", holdErr)
			return
		}
		second()
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("second Hold returned before the first lease was released")
	case <-time.After(30 * time.Millisecond):
	}
	first()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second Hold did not acquire after release")
	}
}

func TestOAuthRefreshLeaseTTLCoversCASHold(t *testing.T) {
	t.Parallel()

	min := time.Duration(maxOAuthRefreshCASAttempts) * oauthRefreshTimeout
	if oauthRefreshLeaseTTL <= min {
		t.Fatalf("oauthRefreshLeaseTTL = %s, want > %s (CAS attempts × token timeout)", oauthRefreshLeaseTTL, min)
	}
}

type fakeOAuthRefreshNXStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newFakeOAuthRefreshNXStore() *fakeOAuthRefreshNXStore {
	return &fakeOAuthRefreshNXStore{values: map[string]string{}}
}

func (s *fakeOAuthRefreshNXStore) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.values[key]; exists {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}

func (s *fakeOAuthRefreshNXStore) CompareAndDelete(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values[key] == value {
		delete(s.values, key)
	}
	return nil
}
