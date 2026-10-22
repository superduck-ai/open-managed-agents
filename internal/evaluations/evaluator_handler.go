package evaluations

import (
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

func (h *Handler) createEvaluator(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, evaluatorNotFound())
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createEvaluatorRequest](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	evaluator, err := newEvaluator(principal, *body)
	if err != nil {
		return err
	}
	created, err := h.store.CreateEvaluationEvaluator(r.Context(), evaluator)
	if err != nil {
		return mapStoreError(err, evaluatorNotFoundMessage, createEvaluatorFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, evaluatorResponseFrom(created))
	return nil
}

func (h *Handler) listEvaluators(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, evaluatorNotFound())
	if err != nil {
		return err
	}
	limit, cursor, err := pageQuery(r, decodeEvaluatorCursor)
	if err != nil {
		return err
	}
	evaluators, hasMore, err := h.store.ListEvaluationEvaluatorsPage(r.Context(), db.ListEvaluationEvaluatorsPageParams{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		Limit:            limit,
		Cursor:           cursor,
	})
	if err != nil {
		return mapStoreError(err, evaluatorNotFoundMessage, listEvaluatorsFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, evaluatorPage(evaluators, hasMore))
	return nil
}

func (h *Handler) getEvaluator(w http.ResponseWriter, r *http.Request) error {
	principal, evaluatorID, err := scopedEvaluatorID(r)
	if err != nil {
		return err
	}
	evaluator, err := h.store.GetEvaluationEvaluator(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, evaluatorID)
	if err != nil {
		return mapStoreError(err, evaluatorNotFoundMessage, loadEvaluatorFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, evaluatorResponseFrom(evaluator))
	return nil
}

func newEvaluator(principal auth.Principal, body createEvaluatorRequest) (db.EvaluationEvaluator, error) {
	normalized, err := normalizeEvaluator(body)
	if err != nil {
		return db.EvaluationEvaluator{}, err
	}
	externalID, err := ids.New("evalev_")
	if err != nil {
		return db.EvaluationEvaluator{}, mapStoreError(err, evaluatorNotFoundMessage, createEvaluatorFailedMessage)
	}
	versionID, err := ids.New("eev_")
	if err != nil {
		return db.EvaluationEvaluator{}, mapStoreError(err, evaluatorNotFoundMessage, createEvaluatorFailedMessage)
	}
	now := time.Now().UTC()
	return db.EvaluationEvaluator{
		UUID:              uuid.NewString(),
		ExternalID:        externalID,
		OrganizationUUID:  principal.OrganizationUUID,
		WorkspaceUUID:     principal.WorkspaceUUID,
		Name:              normalized.Name,
		Description:       normalized.Description,
		CreatedByUserUUID: principal.UserUUID,
		CreatedAt:         now,
		UpdatedAt:         now,
		Version: db.EvaluationEvaluatorVersion{
			UUID:           uuid.NewString(),
			ExternalID:     versionID,
			Version:        1,
			EvaluatorType:  normalized.EvaluatorType,
			Model:          normalized.Model,
			AgentID:        normalized.AgentID,
			PromptTemplate: normalized.PromptTemplate,
			Variables:      normalized.Variables,
			CreatedAt:      now,
		},
	}, nil
}
