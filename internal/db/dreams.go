package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// DreamStatus mirrors the dreams.status lifecycle. Dreams start pending and
// advance to running, then succeed, fail, or are cancelled. Archived dreams
// are terminal and excluded from default lists.
const (
	DreamStatusPending   = "pending"
	DreamStatusRunning   = "running"
	DreamStatusSucceeded = "succeeded"
	DreamStatusFailed    = "failed"
	DreamStatusCancelled = "cancelled"
	DreamStatusArchived  = "archived"
)

// Dream is a managed-agent dream job: an asynchronous distillation of an
// input memory store plus 1~100 sessions into a new output memory store.
// The distillation workflow itself ships later; this struct covers the
// data model and lifecycle persisted by internal/db.
type Dream struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	InputStoreUUID      string
	SessionIDs          []string
	Instructions        *string
	Model               string
	Status              string
	OutputStoreUUID     *string
	Error               *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
}

// DreamPageCursor is the keyset pagination anchor for dreams lists.
type DreamPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

// ListDreamsPageParams controls the dreams list query. Cursor pagination is
// ordered by created_at desc, uuid desc.
type ListDreamsPageParams struct {
	WorkspaceUUID string
	Limit         int
	Cursor        *DreamPageCursor
}

// CreateDream inserts a new pending dream row.
func (d *DB) CreateDream(ctx context.Context, dream Dream) (Dream, error) {
	mapper := NewDreamMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, insertDreamParams{
		UUID:                dream.UUID,
		ExternalID:          dream.ExternalID,
		OrganizationUUID:    dream.OrganizationUUID,
		WorkspaceUUID:       dream.WorkspaceUUID,
		CreatedByAPIKeyUUID: dream.CreatedByAPIKeyUUID,
		InputStoreUUID:      dream.InputStoreUUID,
		SessionIDs:          dreamSessionIDsArg(dream.SessionIDs),
		Instructions:        dream.Instructions,
		Model:               dream.Model,
		Status:              dream.Status,
		CreatedAt:           dream.CreatedAt,
	})
	return dreamFromMapperRow(row, err)
}

// GetDream fetches one dream by its external ID within the workspace.
func (d *DB) GetDream(ctx context.Context, workspaceUUID, externalID string) (Dream, error) {
	mapper := NewDreamMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return dreamFromMapperRow(row, err)
}

// ListDreamsPage lists dreams newest-first with keyset pagination, excluding
// archived rows. It returns the page and whether more rows follow.
func (d *DB) ListDreamsPage(ctx context.Context, params ListDreamsPageParams) ([]Dream, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	filter := listDreamsParams{
		WorkspaceUUID: params.WorkspaceUUID,
		Limit:         params.Limit + 1,
	}
	if params.Cursor != nil {
		filter.HasCursor = true
		filter.CursorCreatedAt = params.Cursor.CreatedAt
		filter.CursorUUID = params.Cursor.UUID
	}

	mapper := NewDreamMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	dreams := dreamsFromMapperRows(rows)
	hasMore := len(dreams) > params.Limit
	if hasMore {
		dreams = dreams[:params.Limit]
	}
	return dreams, hasMore, nil
}

// UpdateDreamStatus transitions a dream to the given status. It requires the
// row to exist, be unarchived, and not already be in the target status; the
// returned rowsAffected is 0 when the transition does not apply. It returns
// ErrNotFound when no such dream exists and ErrInvalidState when the dream
// exists but cannot take the transition.
func (d *DB) UpdateDreamStatus(ctx context.Context, workspaceUUID, externalID, status string) (Dream, int64, error) {
	mapper := NewDreamMapper(d.mapperDB)
	rowsAffected, err := mapper.UpdateStatus(ctx, updateDreamStatusParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    externalID,
		Status:        status,
	})
	if err != nil {
		return Dream{}, 0, err
	}
	if rowsAffected > 0 {
		return d.dreamAfterStatusMutation(ctx, mapper, workspaceUUID, externalID, rowsAffected)
	}
	// 幂等：已处于目标状态（如取消已取消的 dream）返回成功（官方 dreams.md:473）。
	record, findErr := d.GetDream(ctx, workspaceUUID, externalID)
	if findErr != nil {
		if errors.Is(findErr, ErrNotFound) {
			return Dream{}, 0, ErrNotFound
		}
		return Dream{}, 0, findErr
	}
	if record.Status == status {
		return record, 0, nil
	}
	return Dream{}, 0, ErrInvalidState
}

// ArchiveDream marks a dream archived. rowsAffected is 0 when the row does
// not exist, is already archived, or has already reached a terminal status.
func (d *DB) ArchiveDream(ctx context.Context, workspaceUUID, externalID string) (Dream, int64, error) {
	mapper := NewDreamMapper(d.mapperDB)
	rowsAffected, err := mapper.ArchiveByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Dream{}, 0, err
	}
	if rowsAffected > 0 {
		return d.dreamAfterStatusMutation(ctx, mapper, workspaceUUID, externalID, rowsAffected)
	}
	// 已归档幂等（官方 dreams.md:525）：已归档的 dream 再次 archive 是 no-op，返回成功。
	record, findErr := d.GetDream(ctx, workspaceUUID, externalID)
	if findErr != nil {
		if errors.Is(findErr, ErrNotFound) {
			return Dream{}, 0, ErrNotFound
		}
		return Dream{}, 0, findErr
	}
	if record.ArchivedAt != nil {
		return record, 0, nil
	}
	// pending/running 归档 → ErrInvalidState（handler 层映射 400）。
	return Dream{}, 0, ErrInvalidState
}

func (d *DB) dreamAfterStatusMutation(ctx context.Context, mapper DreamMapper, workspaceUUID, externalID string, rowsAffected int64) (Dream, int64, error) {
	if rowsAffected > 0 {
		row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
		dream, err := dreamFromMapperRow(row, err)
		return dream, rowsAffected, err
	}
	_, findErr := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if findErr != nil {
		if errors.Is(findErr, ErrNotFound) || errors.Is(findErr, sql.ErrNoRows) {
			return Dream{}, 0, ErrNotFound
		}
		return Dream{}, 0, findErr
	}
	return Dream{}, 0, ErrInvalidState
}

// SetDreamOutputStore records the output store produced by a completed
// distillation. It requires the dream to be running.
func (d *DB) SetDreamOutputStore(ctx context.Context, workspaceUUID, externalID, outputStoreUUID string) (Dream, error) {
	mapper := NewDreamMapper(d.mapperDB)
	rowsAffected, err := mapper.SetOutputStore(ctx, setDreamOutputStoreParams{
		WorkspaceUUID:   workspaceUUID,
		ExternalID:      externalID,
		OutputStoreUUID: outputStoreUUID,
	})
	if err != nil {
		return Dream{}, err
	}
	return d.dreamAfterMutation(ctx, mapper, workspaceUUID, externalID, rowsAffected)
}

// SetDreamError records a failed distillation error message and moves the
// dream to failed. It requires the dream to be running.
func (d *DB) SetDreamError(ctx context.Context, workspaceUUID, externalID, message string) (Dream, error) {
	mapper := NewDreamMapper(d.mapperDB)
	rowsAffected, err := mapper.SetError(ctx, setDreamErrorParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    externalID,
		Error:         message,
	})
	if err != nil {
		return Dream{}, err
	}
	return d.dreamAfterMutation(ctx, mapper, workspaceUUID, externalID, rowsAffected)
}

func (d *DB) dreamAfterMutation(ctx context.Context, mapper DreamMapper, workspaceUUID, externalID string, rowsAffected int64) (Dream, error) {
	if rowsAffected == 0 {
		_, findErr := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
		if findErr != nil {
			if errors.Is(findErr, ErrNotFound) || errors.Is(findErr, sql.ErrNoRows) {
				return Dream{}, ErrNotFound
			}
			return Dream{}, findErr
		}
		return Dream{}, ErrInvalidState
	}
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return dreamFromMapperRow(row, err)
}

func dreamFromMapperRow(row dreamRow, err error) (Dream, error) {
	if err != nil {
		return Dream{}, mapNoRows(err)
	}
	return row.dream(), nil
}

func dreamsFromMapperRows(rows []dreamRow) []Dream {
	dreams := make([]Dream, len(rows))
	for index := range rows {
		dreams[index] = rows[index].dream()
	}
	return dreams
}

func (r dreamRow) dream() Dream {
	return Dream{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		InputStoreUUID:      r.InputStoreUUID,
		SessionIDs:          dreamSessionIDsValue(r.SessionIDs),
		Instructions:        r.Instructions,
		Model:               r.Model,
		Status:              r.Status,
		OutputStoreUUID:     r.OutputStoreUUID,
		Error:               r.Error,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		ArchivedAt:          r.ArchivedAt,
	}
}

func dreamSessionIDsArg(sessionIDs []string) []byte {
	if len(sessionIDs) == 0 {
		return []byte(`[]`)
	}
	raw, err := json.Marshal(sessionIDs)
	if err != nil {
		return []byte(`[]`)
	}
	return raw
}

func dreamSessionIDsValue(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var sessionIDs []string
	if err := json.Unmarshal(raw, &sessionIDs); err != nil {
		return nil
	}
	return sessionIDs
}
