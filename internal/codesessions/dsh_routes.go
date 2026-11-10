package codesessions

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
)

func (h *Handler) registerDSHRoutes(router chi.Router) {
	router.Route("/dsr", func(router chi.Router) {
		dsh.ConfigureRouter(router)
		h.registerDSHCapabilities(router)
		dsh.NewExecutionHandler(dshExecution{handler: h}, h.authenticateDSH, h.logger).RegisterRoutes(router)
		dsh.NewHandler(h.db, h.authenticateDSH, h.logger.With("component", "dsh")).RegisterRoutes(router)
		dsh.NewControlHandler(h.db, dshLifecycle{handler: h}, h.authenticateDSH, h.brainClaims.authorize, h.logger.With("component", "dsh")).RegisterRoutes(router)
	})
}

func (h *Handler) authenticateDSH(r *http.Request, id string) (db.DSHScope, error) {
	claims, err := h.service.AuthenticateSessionIngress(r.Context(), auth.ExtractAPIKey(r), id)
	if err != nil {
		return db.DSHScope{}, dsh.ErrUnauthenticated
	}
	if claims.Protocol != "dsr1" || claims.WorkerEpoch <= 0 {
		return db.DSHScope{}, dsh.ErrUnauthenticated
	}
	record, err := h.db.GetCodeSessionCredentialContextForIssue(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) && r.Method != http.MethodGet && r.Method != http.MethodHead {
		return db.DSHScope{}, dshUnavailableSessionError(r.Context(), h.db.GetSession, claims)
	}
	if err != nil {
		return db.DSHScope{}, err
	}
	if !credentialContextMatchesClaims(record, claims) {
		return db.DSHScope{}, dsh.ErrUnauthenticated
	}
	return db.DSHScope{CodeSessionID: record.CodeSessionExternalID, SessionID: record.PublicSessionExternalID, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, CodeSessionUUID: record.CodeSessionUUID, Epoch: claims.WorkerEpoch}, nil
}

func dshUnavailableSessionError(ctx context.Context, find func(context.Context, string, string) (db.Session, bool, error), claims SessionCredentialClaims) error {
	session, found, err := find(ctx, claims.WorkspaceUUID, claims.PublicSessionID)
	if err != nil {
		return err
	}
	if !found || session.OrganizationUUID != claims.OrganizationUUID {
		return db.ErrNotFound
	}
	if err := db.DSHSessionWriteError(session); err != nil {
		return err
	}
	return db.ErrNotFound
}
