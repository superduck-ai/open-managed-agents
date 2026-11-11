package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	caseGenerationSystemPrompt = "Reply with one JSON array and no other text. Each item must include query."
	maxSessionEventPages       = 20
)

var errSessionTooLarge = errors.New("session events truncated")

type rubricWriteRequest struct {
	Name      string `json:"name"`
	Desc      string `json:"desc"`
	Body      string `json:"body"`
	PassScore *int   `json:"pass_score"`
	Model     string `json:"model"`
	Goal      string `json:"goal"`
	Example   string `json:"example"`
	AgentID   string `json:"agent_id"`
	DatasetID string `json:"dataset_id"`
	Positive  string `json:"positive"`
	Negative  string `json:"negative"`
}

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

func (h *Handler) createRubric(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[rubricWriteRequest](w, r, 1<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	rubric, err := rubricFromRequest(principal, *body)
	if err != nil {
		return err
	}
	created, err := h.store.CreateEvaluationRubric(r.Context(), rubric)
	if err != nil {
		return mapStoreError(err, rubricNotFoundMessage, createFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, rubricJSON(created))
	return nil
}

func (h *Handler) listRubrics(w http.ResponseWriter, r *http.Request) error {
	principal, offset, limit, err := h.page(r)
	if err != nil {
		return err
	}
	rows, err := h.store.ListEvaluationRubrics(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, offset, limit+1)
	if err != nil {
		return internalFailure(loadFailedMessage, err)
	}
	next := nextPage(offset, limit, len(rows))
	if len(rows) > limit {
		rows = rows[:limit]
	}
	data := make([]rubricResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, rubricJSON(row))
	}
	writePage(w, data, next)
	return nil
}

func (h *Handler) listRubricVersions(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	rubricID := chi.URLParam(r, "rubricId")
	if _, err := h.store.GetEvaluationRubric(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, rubricID); err != nil {
		return mapStoreError(err, rubricNotFoundMessage, loadFailedMessage)
	}
	rows, err := h.store.ListEvaluationRubricVersions(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, rubricID)
	if err != nil {
		return mapStoreError(err, rubricNotFoundMessage, loadFailedMessage)
	}
	data := make([]rubricVersionResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, rubricVersionJSON(row))
	}
	writePage(w, data, nil)
	return nil
}

func (h *Handler) getRubric(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	rubric, err := h.store.GetEvaluationRubric(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "rubricId"))
	if err != nil {
		return mapStoreError(err, rubricNotFoundMessage, loadFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, rubricJSON(rubric))
	return nil
}

func (h *Handler) deleteRubric(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	rubricID := chi.URLParam(r, "rubricId")
	if err := h.store.DeleteEvaluationRubric(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, rubricID, time.Now().UTC()); err != nil {
		return mapStoreError(err, rubricNotFoundMessage, deleteFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": rubricID, "type": "evaluation_rubric_deleted", "deleted": true})
	return nil
}

func (h *Handler) appendRubricVersion(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[rubricWriteRequest](w, r, 1<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	version, err := rubricVersion(*body)
	if err != nil {
		return err
	}
	updated, err := h.store.AppendEvaluationRubricVersion(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "rubricId"), version, time.Now().UTC())
	if err != nil {
		return mapStoreError(err, rubricNotFoundMessage, createFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, rubricJSON(updated))
	return nil
}

func (h *Handler) generateRubric(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[rubricWriteRequest](w, r, 1<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	prompt, err := h.rubricGeneratePrompt(r.Context(), principal, *body)
	if err != nil {
		return err
	}
	text, err := h.complete(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, body.Model, rubricSystemPrompt, prompt)
	if err != nil {
		return err
	}
	body.Body, err = generatedRubricBody(text)
	if err != nil {
		return err
	}
	rubric, err := rubricFromRequest(principal, *body)
	if err != nil {
		return err
	}
	created, err := h.store.CreateEvaluationRubric(r.Context(), rubric)
	if err != nil {
		return mapStoreError(err, rubricNotFoundMessage, createFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, rubricJSON(created))
	return nil
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
	drafts, err := parseCaseDocument(stripCodeFence(text))
	if err != nil {
		return internalFailure(generateFailedMessage, err)
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
		return db.Session{}, nil, notFound(sessionNotFoundMessage, nil)
	}
	events, err := loadSessionEvents(ctx, h.store, principal.WorkspaceUUID, sessionID)
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
			return db.Agent{}, mapStoreError(err, agentNotFoundMessage, generateFailedMessage)
		}
		return agent, nil
	}
	agent, err := h.store.GetAgent(ctx, workspaceUUID, agentID)
	if err != nil {
		return db.Agent{}, mapStoreError(err, agentNotFoundMessage, generateFailedMessage)
	}
	return agent, nil
}

type sessionEventReader interface {
	ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)
}

func loadSessionEvents(ctx context.Context, reader sessionEventReader, workspaceUUID, sessionID string) ([]db.SessionEvent, error) {
	var all []db.SessionEvent
	var cursor *db.SessionEventPageCursor
	for page := 0; page < maxSessionEventPages; page++ {
		events, hasMore, err := reader.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
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

func rubricGenerateReady(body rubricWriteRequest) bool {
	return strings.TrimSpace(body.Goal) != "" ||
		strings.TrimSpace(body.Example) != "" ||
		strings.TrimSpace(body.Positive) != "" ||
		strings.TrimSpace(body.Negative) != "" ||
		strings.TrimSpace(body.DatasetID) != ""
}

func (h *Handler) rubricGeneratePrompt(ctx context.Context, principal auth.Principal, body rubricWriteRequest) (string, error) {
	if !rubricGenerateReady(body) {
		return "", invalidArgument(rubricGenerateRequiredMessage, nil)
	}
	var prompt strings.Builder
	prompt.WriteString("根据目标和样例写可检查的评分标准，包含判断条件、分值和不合格情形。")
	if goal := strings.TrimSpace(body.Goal); goal != "" {
		prompt.WriteString("\n目标：" + goal)
	}
	if agentID := strings.TrimSpace(body.AgentID); agentID != "" {
		agent, err := h.store.GetAgent(ctx, principal.WorkspaceUUID, agentID)
		if err != nil {
			return "", mapStoreError(err, agentNotFoundMessage, generateFailedMessage)
		}
		if agent.System != nil && strings.TrimSpace(*agent.System) != "" {
			prompt.WriteString("\nAgent 系统提示词：" + *agent.System)
		}
	}
	if datasetID := strings.TrimSpace(body.DatasetID); datasetID != "" {
		cases, err := h.store.ListEvaluationCases(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
		if err != nil {
			return "", mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
		}
		limit := len(cases)
		if limit > 12 {
			limit = 12
		}
		for index := 0; index < limit; index++ {
			prompt.WriteString("\n评测集问题：" + cases[index].Query)
		}
	}
	if positive := strings.TrimSpace(body.Positive); positive != "" {
		prompt.WriteString("\n正例：" + positive)
	}
	if negative := strings.TrimSpace(body.Negative); negative != "" {
		prompt.WriteString("\n负例：" + negative)
	}
	if example := strings.TrimSpace(body.Example); example != "" {
		prompt.WriteString("\n样例：" + example)
	}
	return prompt.String(), nil
}

func rubricFromRequest(principal auth.Principal, body rubricWriteRequest) (db.EvaluationRubric, error) {
	name, err := normalizeName(body.Name)
	if err != nil {
		return db.EvaluationRubric{}, err
	}
	description, err := normalizeDescription(body.Desc)
	if err != nil {
		return db.EvaluationRubric{}, err
	}
	version, err := rubricVersion(body)
	if err != nil {
		return db.EvaluationRubric{}, err
	}
	rubricID, versionID, err := newPair("evalrb_", "erv_")
	if err != nil {
		return db.EvaluationRubric{}, err
	}
	now := time.Now().UTC()
	return db.EvaluationRubric{
		UUID: uuid.NewString(), ExternalID: rubricID, OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		Name: name, Description: description, VersionUUID: version.VersionUUID, VersionExternalID: versionID, Body: version.Body,
		PassScore: version.PassScore, Model: version.Model, CreatedAt: now,
	}, nil
}

func generatedRubricBody(text string) (string, error) {
	text = stripMarkdownFence(text)
	if jsonRubricDocument(text) {
		return "", invalidArgument(rubricFormatMessage, nil)
	}
	return text, nil
}

func stripMarkdownFence(text string) string {
	text = strings.TrimSpace(text)
	rest, fenced := strings.CutPrefix(text, "```")
	if !fenced {
		return text
	}
	_, body, found := strings.Cut(rest, "\n")
	if !found {
		return text
	}
	body = strings.TrimSpace(body)
	body, closed := strings.CutSuffix(body, "```")
	if !closed {
		return text
	}
	return strings.TrimSpace(body)
}

func jsonRubricDocument(text string) bool {
	var value any
	if json.Unmarshal([]byte(text), &value) != nil {
		return false
	}
	switch value.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func rubricVersion(body rubricWriteRequest) (db.EvaluationRubric, error) {
	text := strings.TrimSpace(body.Body)
	if text == "" {
		return db.EvaluationRubric{}, invalidArgument(rubricRequiredMessage, nil)
	}
	if utf8.RuneCountInString(text) > maxRubricLength {
		return db.EvaluationRubric{}, invalidArgument(rubricTooLongMessage, nil)
	}
	passScore := 60
	if body.PassScore != nil {
		passScore = *body.PassScore
	}
	if passScore < 0 || passScore > 100 {
		return db.EvaluationRubric{}, invalidArgument(passScoreInvalidMessage, nil)
	}
	model := strings.TrimSpace(body.Model)
	if utf8.RuneCountInString(model) > maxModelLength {
		return db.EvaluationRubric{}, invalidArgument(modelTooLongMessage, nil)
	}
	versionID, err := ids.New("erv_")
	if err != nil {
		return db.EvaluationRubric{}, internalFailure(createFailedMessage, err)
	}
	return db.EvaluationRubric{VersionUUID: uuid.NewString(), VersionExternalID: versionID, Body: text, PassScore: passScore, Model: model}, nil
}

func (h *Handler) complete(ctx context.Context, organizationUUID, workspaceUUID, modelID, system, user string) (string, error) {
	if h.model == nil {
		return "", unavailable(modelMissingMessage, errModelMissing)
	}
	completion, err := h.model.Complete(ctx, CompletionRequest{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Model: modelID, System: system, User: user,
	})
	if err != nil {
		if errors.Is(err, errModelMissing) {
			return "", unavailable(modelMissingMessage, err)
		}
		return "", internalFailure(generateFailedMessage, err)
	}
	return completion.Text, nil
}
