package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/google/uuid"
)

func TestDreamLifecyclePostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil)).With("component", "database")
	database, err := Open(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("open project database: %v", err)
	}
	defer database.Close()

	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin dream lifecycle transaction: %v", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back dream lifecycle transaction: %v", rollbackErr)
		}
	}()

	now := time.Now().UTC()
	externalID := "dream_lifecycle_" + uuid.NewString()[:8]
	created, err := database.CreateDream(ctx, Dream{
		UUID:                uuid.NewString(),
		ExternalID:          externalID,
		OrganizationUUID:    "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000002",
		CreatedByAPIKeyUUID: "00000000-0000-4000-8000-000000000003",
		InputStoreUUID:      "00000000-0000-4000-8000-000000000004",
		SessionIDs:          []string{"sesn_lifecycle_1", "sesn_lifecycle_2"},
		Status:              DreamStatusPending,
		CreatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create dream: %v", err)
	}
	if created.Status != DreamStatusPending || len(created.SessionIDs) != 2 || created.OutputStoreUUID != nil {
		t.Fatalf("unexpected created dream: %+v", created)
	}

	got, err := database.GetDream(ctx, created.WorkspaceUUID, created.ExternalID)
	if err != nil || got.Status != DreamStatusPending {
		t.Fatalf("get dream = (%+v, %v)", got, err)
	}

	t.Run("failure cancel before running is allowed", func(t *testing.T) {
		cancelled, rows, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, created.ExternalID, DreamStatusCancelled)
		if err != nil || rows != 1 || cancelled.Status != DreamStatusCancelled {
			t.Fatalf("UpdateDreamStatus() = (%+v, %d, %v)", cancelled, rows, err)
		}
	})

	t.Run("failure running to running is invalid", func(t *testing.T) {
		_, _, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, created.ExternalID, DreamStatusRunning)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("UpdateDreamStatus() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure second cancel is invalid", func(t *testing.T) {
		_, _, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, created.ExternalID, DreamStatusCancelled)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("UpdateDreamStatus() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("success archive after cancelled", func(t *testing.T) {
		archived, rows, err := database.ArchiveDream(ctx, created.WorkspaceUUID, created.ExternalID)
		if err != nil || rows != 1 || archived.Status != DreamStatusArchived || archived.ArchivedAt == nil {
			t.Fatalf("ArchiveDream() = (%+v, %d, %v)", archived, rows, err)
		}
	})

	t.Run("failure second archive is invalid", func(t *testing.T) {
		_, _, err := database.ArchiveDream(ctx, created.WorkspaceUUID, created.ExternalID)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("ArchiveDream() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure unknown dream not found", func(t *testing.T) {
		_, _, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, "dream_missing", DreamStatusRunning)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateDreamStatus() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("failure output store on archived dream", func(t *testing.T) {
		_, err := database.SetDreamOutputStore(ctx, created.WorkspaceUUID, created.ExternalID, "00000000-0000-4000-8000-000000000099")
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("SetDreamOutputStore() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure error on archived dream", func(t *testing.T) {
		_, err := database.SetDreamError(ctx, created.WorkspaceUUID, created.ExternalID, "distillation exploded")
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("SetDreamError() error = %v, want ErrInvalidState", err)
		}
	})
}

func TestDreamRunningLifecyclePostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	database, err := Open(ctx, cfg, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("open project database: %v", err)
	}
	defer database.Close()

	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin dream running transaction: %v", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back dream running transaction: %v", rollbackErr)
		}
	}()

	now := time.Now().UTC()
	created, err := database.CreateDream(ctx, Dream{
		UUID:                uuid.NewString(),
		ExternalID:          "dream_running_" + uuid.NewString()[:8],
		OrganizationUUID:    "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000002",
		CreatedByAPIKeyUUID: "00000000-0000-4000-8000-000000000003",
		InputStoreUUID:      "00000000-0000-4000-8000-000000000004",
		SessionIDs:          []string{"sesn_running_1"},
		Status:              DreamStatusPending,
		CreatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create dream: %v", err)
	}
	if _, _, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, created.ExternalID, DreamStatusRunning); err != nil {
		t.Fatalf("move dream to running: %v", err)
	}

	t.Run("success output store while running", func(t *testing.T) {
		done, err := database.SetDreamOutputStore(ctx, created.WorkspaceUUID, created.ExternalID, "00000000-0000-4000-8000-000000000099")
		if err != nil || done.OutputStoreUUID == nil || *done.OutputStoreUUID != "00000000-0000-4000-8000-000000000099" {
			t.Fatalf("SetDreamOutputStore() = (%+v, %v)", done, err)
		}
	})

	t.Run("success succeeded while running", func(t *testing.T) {
		done, rows, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, created.ExternalID, DreamStatusSucceeded)
		if err != nil || rows != 1 || done.Status != DreamStatusSucceeded {
			t.Fatalf("UpdateDreamStatus() = (%+v, %d, %v)", done, rows, err)
		}
	})

	t.Run("failure cancel after succeeded", func(t *testing.T) {
		_, _, err := database.UpdateDreamStatus(ctx, created.WorkspaceUUID, created.ExternalID, DreamStatusCancelled)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("UpdateDreamStatus() error = %v, want ErrInvalidState", err)
		}
	})
}
