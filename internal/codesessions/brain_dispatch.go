package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

const (
	brainWorkerTransport       = "claim"
	brainClaimTTL              = 30 * time.Second
	brainClaimMaxAttempts      = 5
	brainClaimExhaustedHold    = time.Minute
	brainClaimPollInterval     = time.Second
	brainClaimMaxBlock         = 30 * time.Second
	brainClaimExhaustedLimit   = 10
	brainWorkerLeaseTTL        = 20 * time.Second
	brainWorkerLeaseGrace      = 10 * time.Second
	brainClaimExhaustedMessage = "The agent worker could not start this session after repeated attempts."
)

type BrainClaimReleaseReason string

const (
	BrainClaimAdmissionFailed BrainClaimReleaseReason = "admission_failed"
	BrainClaimShutdown        BrainClaimReleaseReason = "shutdown"
	BrainClaimPermanent       BrainClaimReleaseReason = "permanent"
)

type BrainClaim struct {
	CodeSessionID  string
	WorkerEpoch    int64
	IngressToken   string
	ModelToken     string
	ClaimExpiresAt time.Time
}

type codeSessionTransportMetadata struct {
	Config struct {
		WorkerTransport string `json:"worker_transport"`
	} `json:"config"`
}

func leaseTimingsForTransport(transport string) db.WorkerLeaseTimings {
	if transport == brainWorkerTransport {
		return db.WorkerLeaseTimings{TTL: brainWorkerLeaseTTL, Grace: brainWorkerLeaseGrace}
	}
	return db.WorkerLeaseTimings{TTL: codeSessionWorkerLeaseTTL, Grace: codeSessionWorkerLeaseGrace}
}

func usesBrainClaimTransport(codeSession db.CodeSession) bool {
	var metadata codeSessionTransportMetadata
	if len(codeSession.Metadata) == 0 || json.Unmarshal(codeSession.Metadata, &metadata) != nil {
		return false
	}
	return metadata.Config.WorkerTransport == brainWorkerTransport
}

func (s *Service) workerLeaseTimings(ctx context.Context, codeSessionID string) (db.WorkerLeaseTimings, error) {
	transport, err := s.db.GetCodeSessionWorkerTransport(ctx, codeSessionID)
	if err != nil {
		return db.WorkerLeaseTimings{}, err
	}
	return leaseTimingsForTransport(transport), nil
}

func (s *Service) brainWorkSignal() workerevents.BrainWorkSignal {
	signal, _ := s.workerEvents.(workerevents.BrainWorkSignal)
	return signal
}

func (s *Service) DispatchWorker(ctx context.Context, session db.Session, codeSessionID string) error {
	if err := s.db.RequestCodeSessionWorker(ctx, session.OrganizationUUID, session.WorkspaceUUID, codeSessionID); err != nil {
		return err
	}
	s.notifyBrainWork(ctx, codeSessionID)
	return nil
}

func (s *Service) notifyBrainWork(ctx context.Context, codeSessionID string) {
	if signal := s.brainWorkSignal(); signal != nil {
		if err := signal.NotifyBrainWork(ctx); err != nil {
			s.logger.WarnContext(ctx, "notify brain workers", "code_session_id", codeSessionID, "error", err)
		}
	}
}

func (s *Service) ClaimBrainWorker(ctx context.Context, workerID string, block time.Duration) (BrainClaim, bool, error) {
	deadline := time.Now().Add(min(block, brainClaimMaxBlock))
	waitCtx, stop := context.WithCancel(ctx)
	defer stop()
	var signals <-chan struct{}
	if signal := s.brainWorkSignal(); signal != nil {
		subscribed, err := signal.SubscribeBrainWork(waitCtx)
		if err != nil {
			s.logger.WarnContext(ctx, "subscribe brain work signal", "error", err)
		}
		signals = subscribed
	}
	for {
		claim, found, err := s.claimNextBrainWorker(ctx, workerID)
		if err != nil || found {
			return claim, found, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return BrainClaim{}, false, nil
		}
		timer := time.NewTimer(min(remaining, brainClaimPollInterval))
		select {
		case <-ctx.Done():
			timer.Stop()
			return BrainClaim{}, false, ctx.Err()
		case <-signals:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (s *Service) claimNextBrainWorker(ctx context.Context, workerID string) (BrainClaim, bool, error) {
	for range brainClaimExhaustedLimit {
		modelToken, err := newOAuthCompatibleToken()
		if err != nil {
			return BrainClaim{}, false, err
		}
		claim, found, err := s.db.ClaimNextCodeSession(ctx, db.ClaimCodeSessionInput{
			WorkerID:             workerID,
			OAuthAccessTokenHash: auth.HashAPIKey(modelToken),
			ClaimTTL:             brainClaimTTL,
			LeaseGrace:           brainWorkerLeaseGrace,
			MaxAttempts:          brainClaimMaxAttempts,
			ExhaustedHold:        brainClaimExhaustedHold,
		})
		if err != nil || !found {
			return BrainClaim{}, false, err
		}
		if claim.Exhausted {
			s.failExhaustedBrainClaim(ctx, claim)
			continue
		}
		credentialContext, err := s.db.GetCodeSessionCredentialContextForIssue(ctx, claim.OrganizationUUID, claim.WorkspaceUUID, claim.CodeSessionExternalID)
		if errors.Is(err, db.ErrNotFound) {
			s.logger.InfoContext(ctx, "claimed code session disappeared before credential issue", "code_session_id", claim.CodeSessionExternalID, "worker_id", workerID)
			continue
		}
		if err != nil {
			return BrainClaim{}, false, err
		}
		ingressToken, err := s.issueSessionIngressToken(credentialContext, claim.WorkerEpoch)
		if err != nil {
			return BrainClaim{}, false, err
		}
		s.logger.InfoContext(ctx, "brain worker claimed code session", "code_session_id", claim.CodeSessionExternalID, "worker_id", workerID, "worker_epoch", claim.WorkerEpoch)
		return BrainClaim{
			CodeSessionID:  claim.CodeSessionExternalID,
			WorkerEpoch:    claim.WorkerEpoch,
			IngressToken:   ingressToken,
			ModelToken:     modelToken,
			ClaimExpiresAt: claim.ClaimExpiresAt,
		}, true, nil
	}
	return BrainClaim{}, false, nil
}

func (s *Service) failExhaustedBrainClaim(ctx context.Context, claim db.CodeSessionClaim) {
	now := formatTime(time.Now().UTC())
	seed := "brain-claim-exhausted:" + strconv.FormatInt(claim.WorkerEpoch, 10)
	payloads := make([]json.RawMessage, 0, 2)
	for _, event := range []map[string]any{
		{"type": "session.error", "error": map[string]any{
			"type": "unknown_error", "message": brainClaimExhaustedMessage, "retry_status": map[string]any{"type": "exhausted"},
		}},
		{"type": "session.status_idle", "stop_reason": map[string]any{"type": "retries_exhausted"}},
	} {
		id := stablePublicEventID(claim.CodeSessionExternalID, seed+":"+event["type"].(string))
		event["id"], event["uuid"] = id, id
		event["created_at"], event["processed_at"], event["timestamp"] = now, now, now
		encoded, err := json.Marshal(event)
		if err != nil {
			s.logger.ErrorContext(ctx, "encode brain claim failure event", "code_session_id", claim.CodeSessionExternalID, "error", err)
			return
		}
		payloads = append(payloads, encoded)
	}
	if err := s.publishPublicPayloads(ctx, claim.CodeSessionExternalID, payloads); err != nil {
		s.logger.ErrorContext(ctx, "publish brain claim failure", "code_session_id", claim.CodeSessionExternalID, "error", err)
		return
	}
	if err := s.db.ResetExhaustedCodeSessionClaim(ctx, claim.CodeSessionUUID, claim.WorkerEpoch); err != nil {
		s.logger.WarnContext(ctx, "reset exhausted brain claim", "code_session_id", claim.CodeSessionExternalID, "worker_epoch", claim.WorkerEpoch, "error", err)
		return
	}
	s.logger.WarnContext(ctx, "brain worker claims exhausted", "code_session_id", claim.CodeSessionExternalID, "attempts", brainClaimMaxAttempts)
}

func (s *Service) ReleaseBrainClaim(ctx context.Context, codeSessionID, workerID string, epoch int64, reason BrainClaimReleaseReason) error {
	if reason == BrainClaimPermanent {
		claim, err := s.db.FailCodeSessionClaimPermanently(ctx, db.FailCodeSessionClaimInput{
			CodeSessionExternalID: codeSessionID, WorkerID: workerID, WorkerEpoch: epoch,
			MaxAttempts: brainClaimMaxAttempts, Hold: brainClaimExhaustedHold,
		})
		if err != nil {
			return err
		}
		s.logger.WarnContext(ctx, "brain worker reported permanent admission failure", "code_session_id", codeSessionID, "worker_id", workerID, "worker_epoch", epoch)
		s.failExhaustedBrainClaim(ctx, claim)
		return nil
	}
	if err := s.db.ReleaseCodeSessionClaim(ctx, codeSessionID, workerID, epoch, reason == BrainClaimAdmissionFailed); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "brain worker released code session", "code_session_id", codeSessionID, "worker_id", workerID, "worker_epoch", epoch, "reason", string(reason))
	if reason == BrainClaimShutdown {
		s.notifyBrainWork(ctx, codeSessionID)
	}
	return nil
}
