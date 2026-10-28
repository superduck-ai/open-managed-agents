package codesessions

import (
	"context"
	"errors"
	"net/http"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/telemetryexport"
)

func (h *Handler) WithTraceExporter(exporter *telemetryexport.Exporter) *Handler {
	h.traceExporter = exporter
	return h
}

func (h *Handler) traceExportSettings(ctx context.Context, organizationUUID, workspaceUUID string) map[string]bool {
	if h.traceExporter == nil {
		return nil
	}
	coverage, err := h.traceExporter.Coverage(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		h.logger.WarnContext(ctx, "load telemetry destinations for worker", "workspace_uuid", workspaceUUID, "error", err)
		return nil
	}
	if !coverage.Enabled {
		return nil
	}
	return map[string]bool{"content": coverage.Content}
}

func (h *Handler) handleCodeSessionWorkerTraces(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admitWorkerOTLP(w, r, h.traceAdmission, h.traceExporter != nil, "trace export is not enabled")
	if !ok {
		return
	}
	defer admitted.release()
	protocol, credentialContext := admitted.protocol, admitted.credentialContext
	body, err := readOTLPRequestBody(w, r, h.cfg.Observability.OTLP.MaxRequestBytes)
	if err != nil {
		status := http.StatusBadRequest
		var bodyError *otlpBodyError
		if errors.As(err, &bodyError) {
			status = bodyError.statusCode
		}
		writeOTLPStatus(w, protocol, status, err.Error(), "")
		return
	}
	request := &coltracepb.ExportTraceServiceRequest{}
	if protocol == otlpProtocolJSON {
		err = (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, request)
	} else {
		err = proto.Unmarshal(body, request)
	}
	if err != nil {
		writeOTLPStatus(w, protocol, http.StatusBadRequest, "invalid OTLP trace payload", "")
		return
	}
	primary, _, err := h.db.GetPrimarySessionThreadExternalID(r.Context(), credentialContext.WorkspaceUUID, credentialContext.PublicSessionExternalID)
	if err != nil {
		h.writeOTLPDatabaseError(w, r, protocol, admitted.codeSessionID, err)
		return
	}
	rejected, err := h.traceExporter.Export(r.Context(), telemetryexport.Source{
		OrganizationUUID: credentialContext.OrganizationUUID, WorkspaceUUID: credentialContext.WorkspaceUUID,
		WorkspaceID: credentialContext.WorkspaceExternalID, SessionID: credentialContext.PublicSessionExternalID,
		CodeSessionID: credentialContext.CodeSessionExternalID, PrimaryThreadID: primary, AgentID: credentialContext.AgentExternalID,
	}, request)
	if err != nil {
		h.logger.WarnContext(r.Context(), "export code session spans", "code_session_id", admitted.codeSessionID, "error", err)
		writeOTLPStatus(w, protocol, http.StatusServiceUnavailable, "trace export queue is unavailable", "")
		return
	}
	response := &coltracepb.ExportTraceServiceResponse{}
	if rejected > 0 {
		response.PartialSuccess = &coltracepb.ExportTracePartialSuccess{RejectedSpans: int64(rejected), ErrorMessage: "spans failed validation"}
	}
	encoded, err := marshalOTLPMessage(protocol, response)
	if err != nil {
		writeOTLPStatus(w, protocol, http.StatusInternalServerError, "could not encode OTLP response", "")
		return
	}
	writeOTLPSuccess(w, protocol, encoded)
}
