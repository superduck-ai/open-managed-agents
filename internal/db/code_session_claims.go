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
			claim.WorkerEpoch = row.CurrentWorkerEpoch
			claim.ClaimExpiresAt = now.Add(input.ExhaustedHold)
			_, err = mapper.HoldExhausted(ctx, row.UUID, claim.ClaimExpiresAt)
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

func (d *DB) ResetExhaustedCodeSessionClaim(ctx context.Context, codeSessionUUID string) error {
	_, err := NewCodeSessionClaimMapper(d.mapperDB).ResetExhausted(ctx, codeSessionUUID)
	return err
}

func (d *DB) ReleaseCodeSessionClaim(ctx context.Context, codeSessionExternalID, workerID string, epoch int64, failed bool) error {
	rows, err := NewCodeSessionClaimMapper(d.mapperDB).Release(ctx, releaseCodeSessionClaimParams{
		ExternalID: codeSessionExternalID,
		WorkerID:   workerID,
		Epoch:      epoch,
		Failed:     failed,
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
