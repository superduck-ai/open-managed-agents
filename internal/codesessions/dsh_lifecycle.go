package codesessions

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
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

func (l dshLifecycle) Context(ctx context.Context, scope db.DSHScope, token, thread string) (json.RawMessage, error) {
	h := l.handler
	record, err := h.requireCodeSession(ctx, scope.CodeSessionID)
	if err != nil {
		return nil, err
	}
	record, err = h.runtimeSessionToolContext(ctx, record, strings.TrimSpace(thread))
	if err != nil {
		return nil, err
	}
	value, err := sessionContextFromCodeSession(record, MCPRuntimeIdentity{Protocol: "dsr1", CodeSessionID: record.ExternalID, SessionIngressToken: token, APIBaseURL: h.cfg.CodeSession.SandboxAPIBaseURL}, h.cfg)
	if err != nil {
		return nil, err
	}
	external, err := h.service.selfHostedCodeSession(ctx, record.ExternalID)
	if err != nil {
		return nil, err
	}
	if external {
		delete(value, "runtime_tool_permissions")
	}
	models, err := h.service.modelCatalog(ctx, record)
	if err != nil {
		return nil, err
	}
	value["models"] = models
	return json.Marshal(value)
}
