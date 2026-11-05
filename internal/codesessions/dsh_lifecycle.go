package codesessions

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
)

type dshLifecycle struct{ handler *Handler }

func (l dshLifecycle) Claim(ctx context.Context, worker string, block time.Duration) (dsh.Claim, bool, error) {
	claim, found, err := l.handler.service.claimBrainWorkerForProtocol(ctx, worker, block, "dsr1")
	if err != nil || !found {
		return dsh.Claim{}, found, err
	}
	return dsh.Claim{CodeSessionID: claim.CodeSessionID, WorkerEpoch: strconv.FormatInt(claim.WorkerEpoch, 10), SessionToken: claim.IngressToken, ModelToken: claim.ModelToken, ClaimExpiresAt: claim.ClaimExpiresAt.Format(time.RFC3339Nano), TraceParent: claim.TraceParent}, true, nil
}

func (l dshLifecycle) Release(ctx context.Context, id, worker string, epoch int64, reason string) error {
	record, found, err := l.handler.db.GetCodeSession(ctx, id)
	if err != nil {
		return err
	}
	var metadata codeSessionTransportMetadata
	if !found || json.Unmarshal(record.Metadata, &metadata) != nil || metadata.Config.WorkerProtocol != "dsr1" {
		return db.ErrNotFound
	}
	return l.handler.service.ReleaseBrainClaim(ctx, id, worker, epoch, BrainClaimReleaseReason(reason))
}

func (l dshLifecycle) Telemetry(ctx context.Context, scope db.DSHScope) (bool, *dsh.TraceExport) {
	settings := l.handler.traceExportSettings(ctx, scope.OrganizationUUID, scope.WorkspaceUUID)
	if settings == nil {
		return l.handler.cfg.Observability.Enabled, nil
	}
	return l.handler.cfg.Observability.Enabled, &dsh.TraceExport{Content: settings["content"]}
}
