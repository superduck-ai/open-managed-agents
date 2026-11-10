package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

const (
	caseGenerationSystemPrompt = "Reply with one JSON array and no other text. Each item must include query."
	maxSessionEventPages       = 20
)

var errSessionTooLarge = errors.New("session events truncated")

type generateCasesRequest struct {
	Count   int    `json:"count"`
	Note    string `json:"note"`
	AgentID string `json:"agent_id"`
	Version int    `json:"agent_version"`
}

type fromSessionRequest struct {
	SessionID      string `json:"session_id"`
	Turns          []int  `json:"turns"`
	Polarity       string `json:"polarity"`
	Acceptance     string `json:"acceptance"`
	ExpectedAnswer string `json:"expected_answer"`
}

type sessionTurnResponse struct {
	Index  int             `json:"index"`
	Query  string          `json:"query"`
	Answer string          `json:"answer"`
	Inputs json.RawMessage `json:"inputs"`
}

type sessionImportResponse struct {
	Data    []caseResponse `json:"data"`
	Skipped int            `json:"skipped"`
}

func (h *Handler) generateCases(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[generateCasesRequest](w, r, 1<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	if body.Count < 1 || body.Count > maxGenerateCases {
		return invalidArgument(generateCountMessage, nil)
	}
	prompt := "生成评测用例 JSON 数组。每条必须有 query，可以有 expectedAnswer。覆盖正常、边界和失败。条数：" + strconv.Itoa(body.Count) + "。补充：" + strings.TrimSpace(body.Note)
	if strings.TrimSpace(body.AgentID) != "" {
		agent, err := h.agentForGeneration(r.Context(), principal.WorkspaceUUID, strings.TrimSpace(body.AgentID), body.Version)
		if err != nil {
			return err
		}
		if agent.System != nil {
			prompt += "\nAgent 系统提示词：" + *agent.System
		}
	}
	text, err := h.complete(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, "", caseGenerationSystemPrompt, prompt)
	if err != nil {
		return err
	}
	drafts, err := parseCaseDocument(text)
	if err != nil {
		return invalidArgument(importInvalidMessage, err)
	}
	if len(drafts) > body.Count {
		drafts = drafts[:body.Count]
	}
	created, err := h.appendCases(r.Context(), principal, chi.URLParam(r, "datasetId"), drafts)
	if err != nil {
		return err
	}
	data := make([]caseResponse, 0, len(created))
	for _, item := range created {
		data = append(data, caseJSON(item))
	}
	writePage(w, data, nil)
	return nil
}

func (h *Handler) listSessionTurns(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	_, turns, err := h.loadSessionTurns(r.Context(), principal, chi.URLParam(r, "datasetId"), sessionID)
	if err != nil {
		return err
	}
	data := make([]sessionTurnResponse, 0, len(turns))
	for _, turn := range turns {
		if !turn.Importable {
			continue
		}
		data = append(data, sessionTurnResponse{Index: turn.Index, Query: turn.Query, Answer: turn.Answer, Inputs: turn.Inputs})
	}
	writePage(w, data, nil)
	return nil
}

func (h *Handler) caseFromSession(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[fromSessionRequest](w, r, 1<<16)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	sessionID := strings.TrimSpace(body.SessionID)
	session, turns, err := h.loadSessionTurns(r.Context(), principal, chi.URLParam(r, "datasetId"), sessionID)
	if err != nil {
		return err
	}
	drafts, skipped, err := sessionCaseDrafts(turns, body.Turns, sessionID, body.ExpectedAnswer, body.Polarity, body.Acceptance)
	if err != nil {
		return err
	}
	var created []db.EvaluationCase
	if len(drafts) > 0 {
		created, err = h.appendCasesInSession(r.Context(), principal, chi.URLParam(r, "datasetId"), drafts, session.UUID)
		if err != nil {
			return err
		}
	}
	data := make([]caseResponse, 0, len(created))
	for _, item := range created {
		data = append(data, caseJSON(item))
	}
	httpapi.WriteJSON(w, http.StatusOK, sessionImportResponse{Data: data, Skipped: skipped})
	return nil
}

func (h *Handler) loadSessionTurns(ctx context.Context, principal auth.Principal, datasetID, sessionID string) (db.Session, []sessionTurn, error) {
	if sessionID == "" {
		return db.Session{}, nil, invalidArgument(sessionRequiredMessage, nil)
	}
	if _, err := h.store.GetEvaluationDataset(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID); err != nil {
		return db.Session{}, nil, mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
	}
	session, ok, err := h.store.GetSession(ctx, principal.WorkspaceUUID, sessionID)
	if err != nil {
		return db.Session{}, nil, internalFailure(loadFailedMessage, err)
	}
	if !ok {
		return db.Session{}, nil, notFound("Session not found", nil)
	}
	events, err := h.sessionEvents(ctx, principal.WorkspaceUUID, sessionID)
	if err != nil {
		if errors.Is(err, errSessionTooLarge) {
			return db.Session{}, nil, invalidArgument(sessionEventsTooLargeMessage, err)
		}
		return db.Session{}, nil, internalFailure(loadFailedMessage, err)
	}
	return session, sessionTurns(events), nil
}

func (h *Handler) agentForGeneration(ctx context.Context, workspaceUUID, agentID string, version int) (db.Agent, error) {
	if version > 0 {
		agent, err := h.store.GetAgentVersion(ctx, workspaceUUID, agentID, version)
		if err != nil {
			return db.Agent{}, mapStoreError(err, "Agent not found", generateFailedMessage)
		}
		return agent, nil
	}
	agent, err := h.store.GetAgent(ctx, workspaceUUID, agentID)
	if err != nil {
		return db.Agent{}, mapStoreError(err, "Agent not found", generateFailedMessage)
	}
	return agent, nil
}

func (h *Handler) sessionEvents(ctx context.Context, workspaceUUID, sessionID string) ([]db.SessionEvent, error) {
	var all []db.SessionEvent
	var cursor *db.SessionEventPageCursor
	for page := 0; page < maxSessionEventPages; page++ {
		events, hasMore, err := h.store.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceUUID: workspaceUUID, SessionExternalID: sessionID, Limit: 100, Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
		lastID := ""
		if len(events) > 0 {
			lastID = events[len(events)-1].ExternalID
		}
		cursor, err = nextSessionEventCursor(page, hasMore, lastID)
		if err != nil {
			return nil, err
		}
		if cursor == nil {
			return all, nil
		}
	}
	return nil, errSessionTooLarge
}

func nextSessionEventCursor(page int, hasMore bool, lastID string) (*db.SessionEventPageCursor, error) {
	if !hasMore || lastID == "" {
		return nil, nil
	}
	if page >= maxSessionEventPages-1 {
		return nil, errSessionTooLarge
	}
	return &db.SessionEventPageCursor{ExternalID: lastID}, nil
}

func (h *Handler) complete(ctx context.Context, organizationUUID, workspaceUUID, modelID, system, user string) (string, error) {
	if h.model == nil {
		return "", unavailable(modelMissingMessage, errModelMissing)
	}
	text, err := h.model.Complete(ctx, CompletionRequest{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Model: modelID, System: system, User: user,
	})
	if err != nil {
		if err == errModelMissing {
			return "", unavailable(modelMissingMessage, err)
		}
		return "", internalFailure(generateFailedMessage, err)
	}
	return text, nil
}
