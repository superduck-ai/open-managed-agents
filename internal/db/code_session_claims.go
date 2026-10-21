package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type ClaimCodeSessionInput struct {
	WorkerID             string
	OAuthAccessTokenHash string
	ClaimTTL             time.Duration
	LeaseGrace           time.Duration
	MaxAttempts          int
	ExhaustedHold        time.Duration
}

type CodeSessionClaim struct {
	CodeSessionUUID       string
	CodeSessionExternalID string
	OrganizationUUID      string
	WorkspaceUUID         string
	SessionExternalID     string
	WorkerEpoch           int64
	ClaimExpiresAt        time.Time
	Exhausted             bool
}

func (d *DB) ClaimNextCodeSession(ctx context.Context, input ClaimCodeSessionInput) (CodeSessionClaim, bool, error) {
	var claim CodeSessionClaim
	var found bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCodeSessionClaimMapper(executor)
		row, ok, err := mapper.LockNextClaimable(ctx, input.LeaseGrace.Microseconds())
		if err != nil || !ok {
			return err
		}
		found = true
		now := time.Now().UTC()
		claim = CodeSessionClaim{
			CodeSessionUUID:       row.UUID,
			CodeSessionExternalID: row.ExternalID,
			OrganizationUUID:      row.OrganizationUUID,
			WorkspaceUUID:         row.WorkspaceUUID,
			SessionExternalID:     row.SessionExternalID,
		}
		if row.WorkerClaimAttempts >= input.MaxAttempts {
			claim.Exhausted = true
			claim.ClaimExpiresAt = now.Add(input.ExhaustedHold)
			claim.WorkerEpoch, err = mapper.HoldExhausted(ctx, row.UUID, claim.ClaimExpiresAt)
			return err
		}
		claim.ClaimExpiresAt = now.Add(input.ClaimTTL)
		claim.WorkerEpoch, err = mapper.Claim(ctx, claimCodeSessionParams{
			UUID:                 row.UUID,
			WorkerID:             input.WorkerID,
			ClaimExpiresAt:       claim.ClaimExpiresAt,
			OAuthAccessTokenHash: input.OAuthAccessTokenHash,
		})
		return err
	})
	if err != nil {
		return CodeSessionClaim{}, false, err
	}
	return claim, found, nil
}

func (d *DB) ResetExhaustedCodeSessionClaim(ctx context.Context, codeSessionUUID string, epoch int64) error {
	rows, err := NewCodeSessionClaimMapper(d.mapperDB).ResetExhausted(ctx, codeSessionUUID, epoch)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPreconditionFailed
	}
	return nil
}

type AckCodeSessionClaimInput struct {
	CodeSessionExternalID string
	WorkerID              string
	WorkerEpoch           int64
	ClaimExpiresAt        time.Time
}

func (d *DB) AckCodeSessionClaim(ctx context.Context, input AckCodeSessionClaimInput) error {
	rows, err := NewCodeSessionClaimMapper(d.mapperDB).Ack(ctx, ackCodeSessionClaimParams{
		ExternalID:     input.CodeSessionExternalID,
		WorkerID:       input.WorkerID,
		Epoch:          input.WorkerEpoch,
		ClaimExpiresAt: input.ClaimExpiresAt,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPreconditionFailed
	}
	return nil
}

type FailCodeSessionClaimInput struct {
	CodeSessionExternalID string
	WorkerID              string
	WorkerEpoch           int64
	MaxAttempts           int
	Hold                  time.Duration
}

func (d *DB) FailCodeSessionClaimPermanently(ctx context.Context, input FailCodeSessionClaimInput) (CodeSessionClaim, error) {
	holdUntil := time.Now().UTC().Add(input.Hold)
	row, err := NewCodeSessionClaimMapper(d.mapperDB).FailPermanently(ctx, failCodeSessionClaimParams{
		ExternalID:  input.CodeSessionExternalID,
		WorkerID:    input.WorkerID,
		Epoch:       input.WorkerEpoch,
		MaxAttempts: input.MaxAttempts,
		HoldUntil:   holdUntil,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSessionClaim{}, ErrPreconditionFailed
	}
	if err != nil {
		return CodeSessionClaim{}, err
	}
	return CodeSessionClaim{
		CodeSessionUUID:       row.UUID,
		CodeSessionExternalID: input.CodeSessionExternalID,
		WorkerEpoch:           row.CurrentWorkerEpoch,
		ClaimExpiresAt:        holdUntil,
		Exhausted:             true,
	}, nil
}

type CodeSessionClaimReleaseKind string

const (
	CodeSessionClaimReleaseRetry   CodeSessionClaimReleaseKind = "retry"
	CodeSessionClaimReleaseHandoff CodeSessionClaimReleaseKind = "handoff"
	CodeSessionClaimReleaseIdle    CodeSessionClaimReleaseKind = "idle"
)

func (d *DB) ReleaseCodeSessionClaim(ctx context.Context, codeSessionExternalID, workerID string, epoch int64, kind CodeSessionClaimReleaseKind) error {
	rows, err := NewCodeSessionClaimMapper(d.mapperDB).Release(ctx, releaseCodeSessionClaimParams{
		ExternalID: codeSessionExternalID,
		WorkerID:   workerID,
		Epoch:      epoch,
		Kind:       kind,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPreconditionFailed
	}
	return nil
}

func (d *DB) RequestCodeSessionWorker(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string) error {
	rows, err := NewCodeSessionClaimMapper(d.mapperDB).Request(ctx, organizationUUID, workspaceUUID, codeSessionExternalID)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) GetCodeSessionWorkerTransport(ctx context.Context, codeSessionExternalID string) (string, error) {
	transport, err := NewCodeSessionClaimMapper(d.mapperDB).FindWorkerTransport(ctx, codeSessionExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return transport, err
}
