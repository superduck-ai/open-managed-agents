package modelcatalog

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestServiceColdStartLoadsSnapshotPublishedByConcurrentInstance(t *testing.T) {
	t.Parallel()
	store := newSharedRefreshStore()
	winnerUpstream := &blockingUpstream{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	winner, err := NewService(context.Background(), store, winnerUpstream, Options{RefreshTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("winner NewService() error = %v", err)
	}
	loserUpstream := &fakeUpstream{err: errors.New("loser must not call upstream")}
	loser, err := NewService(context.Background(), store, loserUpstream, Options{RefreshTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("loser NewService() error = %v", err)
	}

	winnerDone := make(chan error, 1)
	go func() {
		winnerDone <- winner.Refresh(context.Background())
	}()
	<-winnerUpstream.started

	loserDone := make(chan error, 1)
	go func() {
		loserDone <- loser.Refresh(context.Background())
	}()
	<-store.lockMissed
	close(winnerUpstream.release)

	if err := <-winnerDone; err != nil {
		t.Fatalf("winner Refresh() error = %v", err)
	}
	if err := <-loserDone; err != nil {
		t.Fatalf("loser Refresh() error = %v", err)
	}
	if len(loserUpstream.afterIDs) != 0 {
		t.Fatalf("loser upstream calls = %#v, want none", loserUpstream.afterIDs)
	}
	snapshot, err := loser.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("loser Snapshot() error = %v", err)
	}
	if got, want := modelIDs(snapshot.Models), []string{"provider/model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loser model IDs = %#v, want shared snapshot %#v", got, want)
	}
}

func TestServiceRefreshPublishesOnlyCompletePages(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	upstream := &fakeUpstream{pages: map[string]Page{
		"": {
			Models:  []Model{{ID: "provider/a"}, {ID: "provider/b"}},
			HasMore: true,
			LastID:  "provider/b",
		},
		"provider/b": {
			Models: []Model{{ID: "provider/c"}},
		},
	}}
	service, err := NewService(context.Background(), store, upstream, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got, want := modelIDs(snapshot.Models), []string{"provider/a", "provider/b", "provider/c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
	if snapshot.Stale {
		t.Fatal("Snapshot().Stale = true, want false")
	}
	if got, want := upstream.afterIDs, []string{"", "provider/b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("upstream pages = %#v, want %#v", got, want)
	}
	if store.successes != 1 {
		t.Fatalf("successful writes = %d, want 1", store.successes)
	}
}

func TestServiceRefreshRejectsDuplicateModelIDsWithoutPublishing(t *testing.T) {
	t.Parallel()
	lastSuccess := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	store := &fakeStore{
		exists: true,
		snapshot: StoredSnapshot{
			Models:        []Model{{ID: "provider/known"}},
			LastSuccessAt: &lastSuccess,
		},
	}
	service, err := NewService(context.Background(), store, &fakeUpstream{pages: map[string]Page{
		"": {
			Models:  []Model{{ID: "provider/duplicate"}},
			HasMore: true,
			LastID:  "provider/duplicate",
		},
		"provider/duplicate": {
			Models: []Model{{ID: "provider/duplicate"}},
		},
	}}, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want duplicate model ID failure")
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got, want := modelIDs(snapshot.Models), []string{"provider/known"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
	if !snapshot.Stale {
		t.Fatal("Snapshot().Stale = false, want true")
	}
	if store.snapshot.LastError != "invalid_upstream_response" {
		t.Fatalf("LastError = %q, want invalid_upstream_response", store.snapshot.LastError)
	}
}

func TestServiceRefreshKeepsLastSuccessfulSnapshotWhenUpstreamFails(t *testing.T) {
	t.Parallel()
	lastSuccess := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	store := &fakeStore{
		exists: true,
		snapshot: StoredSnapshot{
			Models:        []Model{{ID: "provider/known"}},
			LastSuccessAt: &lastSuccess,
		},
	}
	service, err := NewService(context.Background(), store, &fakeUpstream{err: errors.New("dial failed")}, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want upstream failure")
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got, want := modelIDs(snapshot.Models), []string{"provider/known"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
	if !snapshot.Stale {
		t.Fatal("Snapshot().Stale = false, want true")
	}
	if store.failures != 1 {
		t.Fatalf("failed writes = %d, want 1", store.failures)
	}
}

func TestServiceHasNoFallbackBeforeFirstSuccessfulSnapshot(t *testing.T) {
	t.Parallel()
	service, err := NewService(context.Background(), &fakeStore{}, &fakeUpstream{err: errors.New("unavailable")}, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want upstream failure")
	}
	if _, err := service.Snapshot(context.Background()); !IsUnavailable(err) {
		t.Fatalf("Snapshot() error = %v, want unavailable catalog error", err)
	}
}

func TestServiceRefreshDoesNotPublishWhenSuccessPersistenceFails(t *testing.T) {
	t.Parallel()
	lastSuccess := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	store := &fakeStore{
		exists: true,
		snapshot: StoredSnapshot{
			Models:        []Model{{ID: "provider/known"}},
			LastSuccessAt: &lastSuccess,
		},
		saveErr:    errors.New("database unavailable"),
		failureErr: errors.New("database unavailable"),
	}
	service, err := NewService(context.Background(), store, &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: "provider/new"}}},
	}}, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want persistence failure")
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got, want := modelIDs(snapshot.Models), []string{"provider/known"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want prior snapshot %#v", got, want)
	}
	if !snapshot.Stale || snapshot.LastAttemptAt == nil {
		t.Fatalf("snapshot stale=%v last attempt=%v, want failed refresh metadata", snapshot.Stale, snapshot.LastAttemptAt)
	}
}

func TestServiceRefreshBoundsSuccessPersistenceWithRefreshTimeout(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	service, err := NewService(context.Background(), store, &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: "provider/model"}}},
	}}, Options{RefreshTimeout: time.Minute})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !store.saveHadDeadline {
		t.Fatal("SaveSuccess() context has no deadline, want refresh timeout boundary")
	}
}

func TestServiceSuccessfulRefreshClearsStaleState(t *testing.T) {
	t.Parallel()
	lastSuccess := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	store := &fakeStore{
		exists: true,
		snapshot: StoredSnapshot{
			Models:        []Model{{ID: "provider/old"}},
			LastSuccessAt: &lastSuccess,
		},
	}
	upstream := &fakeUpstream{err: errors.New("gateway unavailable")}
	service, err := NewService(context.Background(), store, upstream, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("first Refresh() error = nil, want upstream failure")
	}
	stale, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("stale Snapshot() error = %v", err)
	}
	if !stale.Stale {
		t.Fatal("Snapshot().Stale = false after failure, want true")
	}

	upstream.err = nil
	upstream.pages = map[string]Page{"": {Models: []Model{{ID: "provider/new"}}}}
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	fresh, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("fresh Snapshot() error = %v", err)
	}
	if fresh.Stale || store.snapshot.LastError != "" {
		t.Fatalf("fresh snapshot stale=%v last error=%q, want cleared", fresh.Stale, store.snapshot.LastError)
	}
	if got, want := modelIDs(fresh.Models), []string{"provider/new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
}

func TestServiceLoadsPersistedSnapshotAfterRestart(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	first, err := NewService(context.Background(), store, &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: "provider/persisted"}}},
	}}, Options{DefaultModelID: "provider/persisted"})
	if err != nil {
		t.Fatalf("first NewService() error = %v", err)
	}
	if err := first.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	restarted, err := NewService(context.Background(), store, &fakeUpstream{err: errors.New("offline")}, Options{
		DefaultModelID: "provider/persisted",
	})
	if err != nil {
		t.Fatalf("restarted NewService() error = %v", err)
	}
	snapshot, err := restarted.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got, want := modelIDs(snapshot.Models), []string{"provider/persisted"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
	if !snapshot.DefaultAvailable || snapshot.DefaultModelID != "provider/persisted" {
		t.Fatalf("default = %q available=%v, want persisted default", snapshot.DefaultModelID, snapshot.DefaultAvailable)
	}
}

func TestServiceClassifiesInvalidModelRecordsWithoutPublishingThem(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	service, err := NewService(context.Background(), store, &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: " invalid "}}},
	}}, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want invalid upstream response")
	}
	if store.snapshot.LastError != "invalid_upstream_response" {
		t.Fatalf("LastError = %q, want invalid_upstream_response", store.snapshot.LastError)
	}
	if _, err := service.Snapshot(context.Background()); !IsUnavailable(err) {
		t.Fatalf("Snapshot() error = %v, want unavailable catalog error", err)
	}
}

func TestServiceUsesOnlyConfiguredDefaultModel(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	service, err := NewService(context.Background(), store, &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: "provider/first"}, {ID: "provider/default"}}},
	}}, Options{DefaultModelID: "provider/default"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.DefaultModelID != "provider/default" {
		t.Fatalf("DefaultModelID = %q, want configured model", snapshot.DefaultModelID)
	}
}

func TestServiceDoesNotInferDefaultFromCatalogOrder(t *testing.T) {
	t.Parallel()
	service, err := NewService(context.Background(), &fakeStore{}, &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: "provider/first"}, {ID: "provider/second"}}},
	}}, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.DefaultAvailable || snapshot.DefaultModelID != "" {
		t.Fatalf("default = %q available=%v, want no inferred default", snapshot.DefaultModelID, snapshot.DefaultAvailable)
	}
}

func TestServiceTryRefreshRejectsConcurrentRefresh(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	upstream := &blockingUpstream{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service, err := NewService(context.Background(), store, upstream, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- service.TryRefresh(context.Background())
	}()
	<-upstream.started

	if err := service.TryRefresh(context.Background()); !errors.Is(err, ErrRefreshInProgress) {
		t.Fatalf("second TryRefresh() error = %v, want refresh in progress", err)
	}
	close(upstream.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first TryRefresh() error = %v", err)
	}
	if store.successes != 1 {
		t.Fatalf("successful writes = %d, want 1", store.successes)
	}
}

func TestServiceTryRefreshHonorsStoreRefreshLock(t *testing.T) {
	t.Parallel()
	store := &lockedFakeStore{fakeStore: &fakeStore{}}
	upstream := &fakeUpstream{pages: map[string]Page{
		"": {Models: []Model{{ID: "provider/model"}}},
	}}
	service, err := NewService(context.Background(), store, upstream, Options{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.TryRefresh(context.Background()); !errors.Is(err, ErrRefreshInProgress) {
		t.Fatalf("TryRefresh() error = %v, want refresh in progress", err)
	}
	if len(upstream.afterIDs) != 0 {
		t.Fatalf("upstream calls = %#v, want none while another instance owns the lock", upstream.afterIDs)
	}
}

func modelIDs(models []Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

type fakeStore struct {
	exists          bool
	snapshot        StoredSnapshot
	successes       int
	failures        int
	saveErr         error
	failureErr      error
	saveHadDeadline bool
}

type lockedFakeStore struct {
	*fakeStore
}

type sharedRefreshStore struct {
	mu         sync.Mutex
	snapshot   StoredSnapshot
	exists     bool
	locked     bool
	lockMissed chan struct{}
	missOnce   sync.Once
}

func newSharedRefreshStore() *sharedRefreshStore {
	return &sharedRefreshStore{lockMissed: make(chan struct{})}
}

func (s *sharedRefreshStore) Load(context.Context) (StoredSnapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStoredSnapshot(s.snapshot), s.exists, nil
}

func (s *sharedRefreshStore) SaveSuccess(_ context.Context, snapshot StoredSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = cloneStoredSnapshot(snapshot)
	s.exists = true
	return nil
}

func (s *sharedRefreshStore) RecordFailure(_ context.Context, attemptedAt time.Time, failure string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.LastAttemptAt = &attemptedAt
	s.snapshot.LastError = failure
	s.exists = true
	return nil
}

func (s *sharedRefreshStore) TryAcquireRefresh(context.Context) (func(), bool, error) {
	s.mu.Lock()
	if s.locked {
		s.mu.Unlock()
		s.missOnce.Do(func() { close(s.lockMissed) })
		return func() {}, false, nil
	}
	s.locked = true
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.locked = false
		s.mu.Unlock()
	}, true, nil
}

func (*lockedFakeStore) TryAcquireRefresh(context.Context) (func(), bool, error) {
	return func() {}, false, nil
}

func (s *fakeStore) Load(context.Context) (StoredSnapshot, bool, error) {
	return s.snapshot, s.exists, nil
}

func (s *fakeStore) SaveSuccess(ctx context.Context, snapshot StoredSnapshot) error {
	_, s.saveHadDeadline = ctx.Deadline()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.snapshot = snapshot
	s.exists = true
	s.successes++
	return nil
}

func (s *fakeStore) RecordFailure(_ context.Context, attemptedAt time.Time, failure string) error {
	if s.failureErr != nil {
		return s.failureErr
	}
	s.snapshot.LastAttemptAt = &attemptedAt
	s.snapshot.LastError = failure
	s.exists = true
	s.failures++
	return nil
}

type fakeUpstream struct {
	pages    map[string]Page
	afterIDs []string
	err      error
}

type blockingUpstream struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (u *blockingUpstream) List(context.Context, string) (Page, error) {
	u.once.Do(func() { close(u.started) })
	<-u.release
	return Page{Models: []Model{{ID: "provider/model"}}}, nil
}

func (u *fakeUpstream) List(_ context.Context, afterID string) (Page, error) {
	u.afterIDs = append(u.afterIDs, afterID)
	if u.err != nil {
		return Page{}, u.err
	}
	page, ok := u.pages[afterID]
	if !ok {
		return Page{}, errors.New("unexpected page")
	}
	return page, nil
}
