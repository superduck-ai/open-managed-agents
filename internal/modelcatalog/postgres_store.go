package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const modelCatalogRefreshAdvisoryLockID int64 = 0x4f4d415f4d4f444c

type postgresStore struct {
	database *db.DB
}

func NewPostgresStore(database *db.DB) Store {
	return &postgresStore{database: database}
}

func (s *postgresStore) Load(ctx context.Context) (StoredSnapshot, bool, error) {
	record, exists, err := s.database.GetModelCatalogSnapshot(ctx, GlobalCatalogKey)
	if err != nil || !exists {
		return StoredSnapshot{}, exists, err
	}
	var models []Model
	if err := json.Unmarshal(record.Models, &models); err != nil {
		return StoredSnapshot{}, false, fmt.Errorf("decode stored model catalog models: %w", err)
	}
	return StoredSnapshot{
		Models:        models,
		LastAttemptAt: record.LastAttemptAt,
		LastSuccessAt: record.LastSuccessAt,
		LastError:     record.LastError,
	}, true, nil
}

func (s *postgresStore) SaveSuccess(ctx context.Context, snapshot StoredSnapshot) error {
	models, err := json.Marshal(snapshot.Models)
	if err != nil {
		return fmt.Errorf("encode model catalog models: %w", err)
	}
	return s.database.SaveModelCatalogSuccess(ctx, db.ModelCatalogSnapshot{
		CatalogKey:    GlobalCatalogKey,
		Models:        models,
		LastAttemptAt: snapshot.LastAttemptAt,
		LastSuccessAt: snapshot.LastSuccessAt,
	})
}

func (s *postgresStore) RecordFailure(ctx context.Context, attemptedAt time.Time, failure string) error {
	return s.database.RecordModelCatalogFailure(ctx, GlobalCatalogKey, attemptedAt, failure)
}

func (s *postgresStore) TryAcquireRefresh(ctx context.Context) (func(), bool, error) {
	// PostgreSQL advisory locks are session-scoped, so lock and unlock must use
	// the same pgx connection instead of the pool-backed sqlx wrapper.
	connection, err := s.database.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := connection.QueryRow(ctx, `select pg_try_advisory_lock($1)`, modelCatalogRefreshAdvisoryLockID).Scan(&acquired); err != nil {
		connection.Release()
		return nil, false, err
	}
	if !acquired {
		connection.Release()
		return func() {}, false, nil
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var unlocked bool
			unlockErr := connection.QueryRow(unlockCtx, `select pg_advisory_unlock($1)`, modelCatalogRefreshAdvisoryLockID).Scan(&unlocked)
			if unlockErr != nil || !unlocked {
				_ = connection.Conn().Close(unlockCtx)
			}
			connection.Release()
		})
	}
	return release, true, nil
}
