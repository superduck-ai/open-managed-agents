package db

import (
	"context"
	"strconv"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type DSHWorkerState struct {
	ModelControlSequence *string
	Model                *string
	TraceTurns           []byte
}

type DSHWorkerPatch struct {
	ControlInputID  string
	ControlSequence string
	ModelSet        bool
	Model           *string
	TraceTurnsSet   bool
	TraceTurns      []byte
}

func (d *DB) RegisterDSHWorker(ctx context.Context, scope DSHScope) (time.Time, error) {
	expiresAt := time.Now().UTC().Add(20 * time.Second)
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		owner, found, err := NewDSHTranscriptMapper(executor).LockWorker(ctx, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if scope.Epoch <= 0 || owner.WorkerEpoch != scope.Epoch {
			return ErrWorkerEpochMismatch
		}
		mapper := NewDSHWorkerMapper(executor)
		rows, err := mapper.Register(ctx, scope, expiresAt)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkerEpochMismatch
		}
		_, err = mapper.EnsureState(ctx, scope)
		return err
	})
	return expiresAt, err
}

func (d *DB) HeartbeatDSHWorker(ctx context.Context, scope DSHScope) (time.Time, error) {
	expiresAt := time.Now().UTC().Add(20 * time.Second)
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		rows, err := NewDSHWorkerMapper(executor).Heartbeat(ctx, scope, expiresAt)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkerEpochMismatch
		}
		return nil
	})
	return expiresAt, err
}

func (d *DB) GetDSHWorkerState(ctx context.Context, scope DSHScope) (DSHWorkerState, error) {
	row, found, err := NewDSHWorkerMapper(d.mapperDB).State(ctx, scope)
	if err != nil {
		return DSHWorkerState{}, err
	}
	if !found {
		return DSHWorkerState{}, nil
	}
	return DSHWorkerState{ModelControlSequence: row.ModelControlSequence, Model: row.Model, TraceTurns: row.TraceTurns}, nil
}

func (d *DB) PatchDSHWorkerState(ctx context.Context, scope DSHScope, patch DSHWorkerPatch) (DSHWorkerState, error) {
	var result DSHWorkerState
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHWorkerMapper(executor)
		if _, err := mapper.EnsureState(ctx, scope); err != nil {
			return err
		}
		if patch.ModelSet {
			var err error
			patch, err = applyDSHModelVersion(ctx, mapper, scope, patch)
			if err != nil {
				return err
			}
		}
		if _, err := mapper.Patch(ctx, scope, patch); err != nil {
			return err
		}
		row, found, err := mapper.State(ctx, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		result = DSHWorkerState{ModelControlSequence: row.ModelControlSequence, Model: row.Model, TraceTurns: row.TraceTurns}
		return nil
	})
	return result, err
}

func applyDSHModelVersion(ctx context.Context, mapper DSHWorkerMapper, scope DSHScope, patch DSHWorkerPatch) (DSHWorkerPatch, error) {
	incoming, err := strconv.ParseUint(patch.ControlSequence, 10, 64)
	if err != nil || incoming == 0 || strconv.FormatUint(incoming, 10) != patch.ControlSequence || patch.ControlInputID == "" {
		return patch, ErrInvalidState
	}
	row, _, err := mapper.State(ctx, scope)
	if err != nil {
		return patch, err
	}
	if row.ModelControlSequence == nil {
		return patch, nil
	}
	current, err := strconv.ParseUint(*row.ModelControlSequence, 10, 64)
	if err != nil {
		return patch, err
	}
	if incoming == current && (row.ModelControlInputID == nil || *row.ModelControlInputID != patch.ControlInputID || !sameDSHParent(row.Model, patch.Model)) {
		return patch, ErrDSHControlConflict
	}
	if incoming <= current {
		patch.ModelSet = false
	}
	return patch, nil
}
