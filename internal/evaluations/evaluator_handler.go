package evaluations

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
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
	if strings.TrimSpace(body.SourceVersionID) != "" {
		return h.createEvaluatorFromPreset(w, r, principal, *body)
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
	return newCustomEvaluator(principal, normalized.Name, normalized.Description, db.EvaluationEvaluatorVersion{
		EvaluatorType:  normalized.EvaluatorType,
		Model:          normalized.Model,
		AgentID:        normalized.AgentID,
		PromptTemplate: normalized.PromptTemplate,
		Variables:      normalized.Variables,
	})
}
