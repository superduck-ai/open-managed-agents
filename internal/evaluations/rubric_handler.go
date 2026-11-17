package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	maxSessionEventPages = 20
	maxRubricCases       = 12
	defaultPassScore     = 60
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

type rubricDraftResponse struct {
	Type      string `json:"type"`
	Body      string `json:"body"`
	PassScore int    `json:"pass_score"`
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
	passScore, err := rubricPassScore(body.PassScore)
	if err != nil {
		return err
	}
	prompt, err := h.rubricGeneratePrompt(r.Context(), principal, *body)
	if err != nil {
		return err
	}
	text, err := h.complete(r.Context(), CompletionRequest{
		OrganizationUUID: principal.OrganizationUUID,
		Model:            strings.TrimSpace(body.Model), System: rubricSystemPrompt, User: prompt,
	})
	if err != nil {
		return err
	}
	generated, err := generatedRubricBody(text)
	if err != nil {
		return err
	}
	if utf8.RuneCountInString(generated) > maxRubricLength {
		return invalidArgument(rubricTooLongMessage, nil)
	}
	httpapi.WriteJSON(w, http.StatusOK, rubricDraftResponse{Type: "evaluation_rubric_draft", Body: generated, PassScore: passScore})
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
	prompt.WriteString(rubricInstructions + "\n" + materialNotice)
	writeOptionalTagged(&prompt, "goal", strings.TrimSpace(body.Goal))
	if agentID := strings.TrimSpace(body.AgentID); agentID != "" {
		agent, err := h.store.GetAgent(ctx, principal.WorkspaceUUID, agentID)
		if err != nil {
			return "", mapStoreError(err, agentNotFoundMessage, generateFailedMessage)
		}
		writeTagged(&prompt, "agent", agentBrief(agent))
	}
	if datasetID := strings.TrimSpace(body.DatasetID); datasetID != "" {
		cases, err := h.store.ListEvaluationCases(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
		if err != nil {
			return "", mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
		}
		writeOptionalTagged(&prompt, "cases", rubricCaseLines(spreadCases(cases, maxRubricCases)))
	}
	writeOptionalTagged(&prompt, "positive_example", strings.TrimSpace(body.Positive))
	writeOptionalTagged(&prompt, "negative_example", strings.TrimSpace(body.Negative))
	writeOptionalTagged(&prompt, "sample", strings.TrimSpace(body.Example))
	return prompt.String(), nil
}

func writeOptionalTagged(builder *strings.Builder, tag, content string) {
	if content != "" {
		writeTagged(builder, tag, content)
	}
}

func rubricCaseLines(cases []db.EvaluationCase) string {
	blocks := make([]string, 0, len(cases))
	for _, item := range cases {
		lines := []string{generationListMarker + "问题：" + clipRunes(item.Query, maxSeedQueryRunes)}
		if item.ExpectedAnswer != "" {
			lines = append(lines, "  期望回答："+clipRunes(item.ExpectedAnswer, maxSeedQueryRunes))
		}
		if len(item.ExpectedToolTrace) > 0 {
			lines = append(lines, "  期望工具："+clipRunes(string(item.ExpectedToolTrace), maxSeedAnswerRunes))
		}
		if item.Acceptance != "" {
			lines = append(lines, "  验收要点："+strings.ReplaceAll(clipRunes(item.Acceptance, maxSeedQueryRunes), "\n", "；"))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n")
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
	passScore, err := rubricPassScore(body.PassScore)
	if err != nil {
		return db.EvaluationRubric{}, err
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

func rubricPassScore(value *int) (int, error) {
	if value == nil {
		return defaultPassScore, nil
	}
	if *value < 0 || *value > 100 {
		return 0, invalidArgument(passScoreInvalidMessage, nil)
	}
	return *value, nil
}

func (h *Handler) complete(ctx context.Context, request CompletionRequest) (string, error) {
	if h.model == nil {
		return "", unavailable(modelMissingMessage, errModelMissing)
	}
	completion, err := h.model.Complete(ctx, request)
	if err != nil {
		if errors.Is(err, errModelMissing) {
			return "", unavailable(modelMissingMessage, err)
		}
		if errors.Is(err, errModelRejected) {
			return "", unavailable(modelRejectedMessage, err)
		}
		return "", internalFailure(generateFailedMessage, err)
	}
	return completion.Text, nil
}
