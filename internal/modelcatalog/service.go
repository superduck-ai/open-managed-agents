package modelcatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultRefreshInterval     = 5 * time.Minute
	defaultRefreshTimeout      = 15 * time.Second
	sharedSnapshotPollInterval = 100 * time.Millisecond
	maxPagesPerRefresh         = 1000
)

var errIncompletePage = errors.New("incomplete model catalog page")

type Service struct {
	store    Store
	upstream Upstream
	options  Options

	mu       sync.RWMutex
	snapshot StoredSnapshot
	exists   bool

	refreshMu sync.Mutex
}

func NewService(ctx context.Context, store Store, upstream Upstream, options Options) (*Service, error) {
	if store == nil {
		return nil, errors.New("model catalog store is required")
	}
	if upstream == nil {
		return nil, errors.New("model catalog upstream is required")
	}
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = defaultRefreshInterval
	}
	if options.RefreshTimeout <= 0 {
		options.RefreshTimeout = defaultRefreshTimeout
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	stored, exists, err := store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load model catalog snapshot: %w", err)
	}
	if exists {
		if _, err := normalizeModels(stored.Models); err != nil {
			return nil, fmt.Errorf("stored model catalog snapshot: %w", err)
		}
	}

	return &Service{
		store:    store,
		upstream: upstream,
		options:  options,
		snapshot: cloneStoredSnapshot(stored),
		exists:   exists,
	}, nil
}

func (s *Service) StartRefreshLoop(ctx context.Context, report func(error)) {
	go func() {
		ticker := time.NewTicker(s.options.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.TryRefresh(ctx); err != nil && !errors.Is(err, ErrRefreshInProgress) && report != nil {
					report(err)
				}
			}
		}
	}()
}

func (s *Service) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refresh(ctx, true)
}

func (s *Service) TryRefresh(ctx context.Context) error {
	if !s.refreshMu.TryLock() {
		return ErrRefreshInProgress
	}
	defer s.refreshMu.Unlock()
	return s.refresh(ctx, false)
}

func (s *Service) refresh(ctx context.Context, waitForSharedSnapshot bool) error {
	refreshCtx, cancel := context.WithTimeout(ctx, s.options.RefreshTimeout)
	defer cancel()
	if locker, ok := s.store.(RefreshLocker); ok {
		release, acquired, err := locker.TryAcquireRefresh(refreshCtx)
		if err != nil {
			return fmt.Errorf("acquire model catalog refresh lock: %w", err)
		}
		if !acquired {
			if waitForSharedSnapshot && !s.hasSuccessfulSnapshot() {
				return s.waitForFirstSharedSnapshot(refreshCtx)
			}
			return ErrRefreshInProgress
		}
		defer release()
	}

	models, err := s.fetchAll(refreshCtx)
	if err != nil {
		if recordErr := s.recordFailure(ctx, failureCategory(err)); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		return err
	}

	now := s.options.Now().UTC()
	stored := StoredSnapshot{
		Models:        cloneModels(models),
		LastAttemptAt: &now,
		LastSuccessAt: &now,
	}
	if err := s.store.SaveSuccess(refreshCtx, stored); err != nil {
		persistErr := fmt.Errorf("persist successful model catalog refresh: %w", err)
		if recordErr := s.recordFailure(ctx, "persistence_unavailable"); recordErr != nil {
			return errors.Join(persistErr, recordErr)
		}
		return persistErr
	}

	s.mu.Lock()
	s.snapshot = cloneStoredSnapshot(stored)
	s.exists = true
	s.mu.Unlock()
	return nil
}

func (s *Service) hasSuccessfulSnapshot() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exists && s.snapshot.LastSuccessAt != nil
}

func (s *Service) waitForFirstSharedSnapshot(ctx context.Context) error {
	ticker := time.NewTicker(sharedSnapshotPollInterval)
	defer ticker.Stop()
	for {
		loaded, err := s.loadSuccessfulSnapshot(ctx)
		if err != nil {
			return err
		}
		if loaded {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrRefreshInProgress, context.Cause(ctx))
		case <-ticker.C:
		}
	}
}

func (s *Service) loadSuccessfulSnapshot(ctx context.Context) (bool, error) {
	stored, exists, err := s.store.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("reload model catalog snapshot: %w", err)
	}
	if !exists || stored.LastSuccessAt == nil {
		return false, nil
	}
	normalized, err := normalizeModels(stored.Models)
	if err != nil {
		return false, fmt.Errorf("stored model catalog snapshot: %w", err)
	}
	stored.Models = normalized
	s.mu.Lock()
	s.snapshot = cloneStoredSnapshot(stored)
	s.exists = true
	s.mu.Unlock()
	return true, nil
}

func (s *Service) Snapshot(context.Context) (Snapshot, error) {
	s.mu.RLock()
	stored := cloneStoredSnapshot(s.snapshot)
	exists := s.exists
	s.mu.RUnlock()
	if !exists || stored.LastSuccessAt == nil {
		return Snapshot{}, ErrUnavailable
	}

	defaultModelID, defaultAvailable := configuredDefault(stored.Models, s.options.DefaultModelID)
	return Snapshot{
		Models:           cloneModels(stored.Models),
		DefaultModelID:   defaultModelID,
		LastAttemptAt:    cloneTime(stored.LastAttemptAt),
		LastSuccessAt:    cloneTime(stored.LastSuccessAt),
		Stale:            stored.LastError != "",
		DefaultAvailable: defaultAvailable,
	}, nil
}

func (s *Service) ValidateModel(ctx context.Context, modelID string) error {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	for _, model := range snapshot.Models {
		if model.ID == modelID {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrUnknownModel, modelID)
}

func (s *Service) fetchAll(ctx context.Context) ([]Model, error) {
	models := make([]Model, 0)
	seenModelIDs := make(map[string]struct{})
	seenCursors := make(map[string]struct{})
	afterID := ""

	for pageNumber := 0; pageNumber < maxPagesPerRefresh; pageNumber++ {
		page, err := s.upstream.List(ctx, afterID)
		if err != nil {
			return nil, fmt.Errorf("list upstream models: %w", err)
		}
		normalized, err := normalizeModels(page.Models)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidUpstreamResponse, err)
		}
		for _, model := range normalized {
			if _, alreadySeen := seenModelIDs[model.ID]; alreadySeen {
				return nil, fmt.Errorf("%w: duplicate model id %q", errInvalidUpstreamResponse, model.ID)
			}
			seenModelIDs[model.ID] = struct{}{}
			models = append(models, model)
		}
		if !page.HasMore {
			return models, nil
		}

		nextCursor := strings.TrimSpace(page.LastID)
		if nextCursor == "" {
			return nil, errIncompletePage
		}
		if _, repeated := seenCursors[nextCursor]; repeated {
			return nil, errIncompletePage
		}
		seenCursors[nextCursor] = struct{}{}
		afterID = nextCursor
	}
	return nil, errIncompletePage
}

func (s *Service) recordFailure(ctx context.Context, failure string) error {
	attemptedAt := s.options.Now().UTC()
	persistErr := s.store.RecordFailure(ctx, attemptedAt, failure)
	s.mu.Lock()
	if !s.exists {
		s.snapshot = StoredSnapshot{}
		s.exists = true
	}
	s.snapshot.LastAttemptAt = &attemptedAt
	s.snapshot.LastError = failure
	s.mu.Unlock()
	if persistErr != nil {
		return fmt.Errorf("persist model catalog refresh failure: %w", persistErr)
	}
	return nil
}

func normalizeModels(models []Model) ([]Model, error) {
	normalized := make([]Model, 0, len(models))
	for _, model := range models {
		if model.ID == "" || model.ID != strings.TrimSpace(model.ID) {
			return nil, errors.New("model id must be a non-empty trimmed string")
		}
		if strings.TrimSpace(model.DisplayName) == "" {
			model.DisplayName = model.ID
		}
		normalized = append(normalized, cloneModel(model))
	}
	return normalized, nil
}

func configuredDefault(models []Model, configuredID string) (string, bool) {
	if configuredID == "" {
		return "", false
	}
	for _, model := range models {
		if model.ID == configuredID {
			return configuredID, true
		}
	}
	return "", false
}

func failureCategory(err error) string {
	if errors.Is(err, errIncompletePage) || errors.Is(err, errInvalidUpstreamResponse) {
		return "invalid_upstream_response"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout"
	}
	return "upstream_unavailable"
}

func cloneStoredSnapshot(snapshot StoredSnapshot) StoredSnapshot {
	return StoredSnapshot{
		Models:        cloneModels(snapshot.Models),
		LastAttemptAt: cloneTime(snapshot.LastAttemptAt),
		LastSuccessAt: cloneTime(snapshot.LastSuccessAt),
		LastError:     snapshot.LastError,
	}
}

func cloneModels(models []Model) []Model {
	cloned := make([]Model, len(models))
	for index, model := range models {
		cloned[index] = cloneModel(model)
	}
	return cloned
}

func cloneModel(model Model) Model {
	model.MaxInputTokens = cloneInt(model.MaxInputTokens)
	model.MaxTokens = cloneInt(model.MaxTokens)
	model.Capabilities = cloneCapabilities(model.Capabilities)
	return model
}

func cloneCapabilities(capabilities Capabilities) Capabilities {
	return Capabilities{
		Thinking:         cloneBool(capabilities.Thinking),
		AdaptiveThinking: cloneBool(capabilities.AdaptiveThinking),
		ToolUse:          cloneBool(capabilities.ToolUse),
		fields:           cloneCapabilityFields(capabilities.fields),
	}
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
