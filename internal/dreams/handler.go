package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	dreamingBeta       = "dreaming-2026-04-21"
	maxDreamsBody      = 1 << 20
	maxDreamSessions   = 100
	defaultDreamsLimit = 20
	maxDreamsLimit     = 100
)

// Handler serves /v1/dreams. Dreams are asynchronous jobs that distill an
// input memory store plus sessions into a new output memory store; the
// distillation workflow itself ships later and advances rows through the
// markDream* status helpers.
type Handler struct {
	db           *db.DB
	logger       *slog.Logger
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

type createDreamRequest struct {
	Inputs       []dreamInputRequest `json:"inputs"`
	Instructions json.RawMessage     `json:"instructions"`
	Model        json.RawMessage     `json:"model"`
}

type dreamInputRequest struct {
	Type          string   `json:"type"`
	MemoryStoreID string   `json:"memory_store_id"`
	SessionIDs    []string `json:"session_ids"`
}

type dreamResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Inputs       []dreamInputResponse `json:"inputs"`
	Instructions *string              `json:"instructions,omitempty"`
	Model        string               `json:"model"`
	Status       string               `json:"status"`
	Outputs      []any                `json:"outputs"`
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
	ArchivedAt   *string              `json:"archived_at"`
	Error        *string              `json:"error,omitempty"`
}

type dreamInputResponse struct {
	Type          string   `json:"type"`
	MemoryStoreID string   `json:"memory_store_id,omitempty"`
	SessionIDs    []string `json:"session_ids,omitempty"`
}

type dreamPageResponse struct {
	Data     []dreamResponse `json:"data"`
	NextPage *string         `json:"next_page"`
}

func NewHandler(database *db.DB, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{db: database, logger: logger, errorAdapter: httpapi.NewErrorAdapter(logger)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(func(http.ResponseWriter, *http.Request) error { return dreamRouteNotFound() }))
	router.MethodNotAllowed(wrap(func(http.ResponseWriter, *http.Request) error { return dreamRouteNotFound() }))
	router.Post("/", wrap(h.create))
	router.Get("/", wrap(h.list))
	router.Get("/{dream_id}", wrap(h.retrieveRoute))
	router.Post("/{dream_id}/cancel", wrap(h.cancelRoute))
	router.Post("/{dream_id}/archive", wrap(h.archiveRoute))
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !hasDreamingBeta(r) {
		h.errorAdapter.Write(w, r, dreamsBetaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func hasDreamingBeta(r *http.Request) bool {
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == dreamingBeta {
				return true
			}
		}
	}
	return false
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireDreamManager(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createDreamRequest](w, r, maxDreamsBody)
	if err != nil {
		return invalidRequest(err)
	}
	inputs, instructions, err := validateCreateDreamRequest(body)
	if err != nil {
		return invalidRequest(err)
	}
	store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceUUID, inputs.storeID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return memoryStoreNotFound(inputs.storeID, err)
		}
		return internalError("Could not create dream", fmt.Errorf("get memory store %q for dream: %w", inputs.storeID, err))
	}
	if store.ArchivedAt != nil {
		return invalidRequest(errors.New("memory store must not be archived"))
	}
	for _, sessionID := range inputs.sessionIDs {
		_, found, sessionErr := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
		if sessionErr != nil {
			return internalError("Could not create dream", fmt.Errorf("check session %q for dream: %w", sessionID, sessionErr))
		}
		if !found {
			return sessionNotFound(sessionID, nil)
		}
	}
	dreamID, err := ids.New("drm_")
	if err != nil {
		return internalError("Could not create dream", fmt.Errorf("generate dream id: %w", err))
	}
	now := time.Now().UTC()
	created, err := h.db.CreateDream(r.Context(), db.Dream{
		UUID:                uuid.NewString(),
		ExternalID:          dreamID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		InputStoreUUID:      store.UUID,
		SessionIDs:          inputs.sessionIDs,
		Instructions:        instructions,
		Model:               inputs.model,
		Status:              db.DreamStatusPending,
		CreatedAt:           now,
	})
	if err != nil {
		return internalError("Could not create dream", fmt.Errorf("insert dream: %w", err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(created, &inputs))
	return nil
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) error {
	return h.retrieve(w, r, chi.URLParam(r, "dream_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, dreamID string) error {
	principal, err := requireDreamManager(r)
	if err != nil {
		return err
	}
	record, err := h.db.GetDream(r.Context(), principal.WorkspaceUUID, dreamID)
	if err != nil {
		return mapDreamLoadError(err, dreamID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record, nil))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireDreamManager(r)
	if err != nil {
		return err
	}
	limit, err := httpapi.ParseLimit(r, maxDreamsLimit)
	if err != nil {
		return invalidRequest(err)
	}
	params := db.ListDreamsPageParams{WorkspaceUUID: principal.WorkspaceUUID, Limit: limit}
	cursor := r.URL.Query().Get("page")
	if cursor != "" {
		parsed, decodeErr := decodeDreamPageCursor(cursor)
		if decodeErr != nil {
			return invalidRequest(decodeErr)
		}
		params.Cursor = parsed
	}
	records, hasMore, err := h.db.ListDreamsPage(r.Context(), params)
	if err != nil {
		return internalError("Could not list dreams", fmt.Errorf("list dreams: %w", err))
	}
	data := make([]dreamResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromDream(record, nil))
	}
	var nextPage *string
	if hasMore && len(data) > 0 {
		last := data[len(data)-1]
		page, encodeErr := encodeDreamPageCursor(db.DreamPageCursor{
			CreatedAt: mustParseTime(last.CreatedAt),
			UUID:      last.ID,
		})
		if encodeErr != nil {
			return internalError("Could not list dreams", fmt.Errorf("encode dreams page cursor: %w", encodeErr))
		}
		nextPage = &page
	}
	httpapi.WriteJSON(w, http.StatusOK, dreamPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) cancelRoute(w http.ResponseWriter, r *http.Request) error {
	return h.cancel(w, r, chi.URLParam(r, "dream_id"))
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request, dreamID string) error {
	principal, err := requireDreamManager(r)
	if err != nil {
		return err
	}
	record, _, err := h.db.UpdateDreamStatus(r.Context(), principal.WorkspaceUUID, dreamID, db.DreamStatusCancelled)
	if err != nil {
		return mapDreamTransitionError(err, dreamID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record, nil))
	return nil
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) error {
	return h.archive(w, r, chi.URLParam(r, "dream_id"))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request, dreamID string) error {
	principal, err := requireDreamManager(r)
	if err != nil {
		return err
	}
	record, _, err := h.db.ArchiveDream(r.Context(), principal.WorkspaceUUID, dreamID)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			// 官方契约（dreams.md:525）：pending/running 归档返回 400。
			return apperr.New(apperr.InvalidArgument, "only terminal dreams (succeeded, failed, cancelled) can be archived", err)
		}
		return mapDreamTransitionError(err, dreamID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record, nil))
	return nil
}

// markDreamRunning advances a dream to running for the distillation worker.
// Transitions apply only while the dream is pending.
func (h *Handler) markDreamRunning(record db.Dream) (db.Dream, error) {
	dream, _, err := h.db.UpdateDreamStatus(context.Background(), record.WorkspaceUUID, record.ExternalID, db.DreamStatusRunning)
	return dream, err
}

// markDreamSucceeded records a completed distillation and its output store.
// Transitions apply only while the dream is running.
func (h *Handler) markDreamSucceeded(record db.Dream, outputStoreUUID string) (db.Dream, error) {
	// 先写 output store（SetOutputStore 要求 status=running），再置 succeeded。
	if _, err := h.db.SetDreamOutputStore(context.Background(), record.WorkspaceUUID, record.ExternalID, outputStoreUUID); err != nil {
		return db.Dream{}, err
	}
	dream, _, err := h.db.UpdateDreamStatus(context.Background(), record.WorkspaceUUID, record.ExternalID, db.DreamStatusSucceeded)
	if err != nil {
		return db.Dream{}, err
	}
	return dream, nil
}

// markDreamFailed records a failed distillation and its error message.
// Transitions apply only while the dream is running.
func (h *Handler) markDreamFailed(record db.Dream, message string) (db.Dream, error) {
	return h.db.SetDreamError(context.Background(), record.WorkspaceUUID, record.ExternalID, message)
}

// markDreamRunningForWorker keeps the distillation transition helpers
// referenced until the worker entrypoint lands; the call site will move into
// the worker once the distillation workflow ships.
var markDreamRunningForWorker = func(_ *Handler, _ db.Dream) (db.Dream, error) {
	return db.Dream{}, nil
}

func init() {
	_ = markDreamRunningForWorker
	_ = (*Handler).markDreamRunning
	_ = (*Handler).markDreamSucceeded
	_ = (*Handler).markDreamFailed
}

type dreamCreateInputs struct {
	storeID    string
	sessionIDs []string
	model      string
}

func validateCreateDreamRequest(body *createDreamRequest) (dreamCreateInputs, *string, error) {
	if body.Inputs == nil {
		return dreamCreateInputs{}, nil, errors.New("inputs must be an array")
	}
	var inputs dreamCreateInputs
	foundStore := false
	for _, input := range body.Inputs {
		switch input.Type {
		case "memory_store":
			if foundStore {
				return dreamCreateInputs{}, nil, errors.New("dream requires exactly one memory store input")
			}
			storeID := strings.TrimSpace(input.MemoryStoreID)
			if storeID == "" {
				return dreamCreateInputs{}, nil, errors.New("memory store input requires memory_store_id")
			}
			inputs.storeID = storeID
			foundStore = true
		case "sessions":
			if len(inputs.sessionIDs) > 0 {
				return dreamCreateInputs{}, nil, errors.New("dream requires exactly one sessions input")
			}
			if len(input.SessionIDs) == 0 {
				return dreamCreateInputs{}, nil, errors.New("sessions input requires at least one session")
			}
			if len(input.SessionIDs) > maxDreamSessions {
				return dreamCreateInputs{}, nil, fmt.Errorf("dream supports at most %d sessions", maxDreamSessions)
			}
			for _, sessionID := range input.SessionIDs {
				if strings.TrimSpace(sessionID) == "" {
					return dreamCreateInputs{}, nil, errors.New("sessions input requires non-empty session ids")
				}
			}
			inputs.sessionIDs = input.SessionIDs
		default:
			return dreamCreateInputs{}, nil, fmt.Errorf("unsupported dream input type %q", input.Type)
		}
	}
	if !foundStore {
		return dreamCreateInputs{}, nil, errors.New("dream requires a memory store input")
	}
	if len(inputs.sessionIDs) == 0 {
		return dreamCreateInputs{}, nil, errors.New("dream requires a sessions input")
	}
	instructions, err := parseInstructions(body.Instructions)
	if err != nil {
		return dreamCreateInputs{}, nil, err
	}
	var model string
	if err := json.Unmarshal(body.Model, &model); err != nil || strings.TrimSpace(model) == "" {
		return dreamCreateInputs{}, nil, errors.New("model is required")
	}
	inputs.model = strings.TrimSpace(model)
	return inputs, instructions, nil
}

func parseInstructions(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var instructions string
	if err := json.Unmarshal(raw, &instructions); err != nil {
		return nil, errors.New("instructions must be a string")
	}
	if strings.TrimSpace(instructions) == "" {
		return nil, errors.New("instructions must not be empty")
	}
	return &instructions, nil
}

func requireDreamManager(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return auth.Principal{}, dreamAuthenticationRequired()
	}
	if principal.CredentialType != "" &&
		principal.CredentialType != auth.CredentialTypeAPIKey &&
		principal.CredentialType != auth.CredentialTypePlatformSession {
		return auth.Principal{}, apperr.New(apperr.PermissionDenied, "API key authentication required", nil)
	}
	return principal, nil
}

func mapDreamTransitionError(err error, dreamID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return dreamNotFound(dreamID, err)
	}
	if errors.Is(err, db.ErrInvalidState) {
		return dreamCannotTransition(err)
	}
	return mapDreamLoadError(err, dreamID)
}

func decodeDreamPageCursor(raw string) (*db.DreamPageCursor, error) {
	parts := strings.SplitN(raw, "_", 2)
	if len(parts) != 2 {
		return nil, errors.New("page cursor must be created_at_uuid")
	}
	createdAt, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return nil, errors.New("page cursor must start with an RFC3339 created_at")
	}
	if strings.TrimSpace(parts[1]) == "" {
		return nil, errors.New("page cursor must include a dream id")
	}
	return &db.DreamPageCursor{CreatedAt: createdAt.UTC(), UUID: parts[1]}, nil
}

func encodeDreamPageCursor(cursor db.DreamPageCursor) (string, error) {
	return cursor.CreatedAt.UTC().Format(time.RFC3339) + "_" + cursor.UUID, nil
}

func mustParseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func responseFromDream(record db.Dream, inputs *dreamCreateInputs) dreamResponse {
	var inputResponses []dreamInputResponse
	if inputs != nil {
		inputResponses = []dreamInputResponse{
			{Type: "memory_store", MemoryStoreID: inputs.storeID},
			{Type: "sessions", SessionIDs: inputs.sessionIDs},
		}
	} else {
		inputResponses = dreamInputsFromRecord(record)
	}
	outputs := make([]any, 0)
	if record.OutputStoreUUID != nil && *record.OutputStoreUUID != "" {
		outputs = append(outputs, map[string]any{
			"type":            "memory_store",
			"memory_store_id": record.OutputStoreUUID,
		})
	}
	response := dreamResponse{
		ID:           record.ExternalID,
		Type:         "dream",
		Inputs:       inputResponses,
		Instructions: record.Instructions,
		Model:        record.Model,
		Status:       record.Status,
		Outputs:      outputs,
		CreatedAt:    httpapi.FormatTime(record.CreatedAt),
		UpdatedAt:    httpapi.FormatTime(record.UpdatedAt),
		ArchivedAt:   httpapi.OptionalTime(record.ArchivedAt),
		Error:        record.Error,
	}
	return response
}

func dreamInputsFromRecord(record db.Dream) []dreamInputResponse {
	return []dreamInputResponse{
		{Type: "memory_store", MemoryStoreID: record.InputStoreUUID},
		{Type: "sessions", SessionIDs: record.SessionIDs},
	}
}
