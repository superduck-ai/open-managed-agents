// Package dreams exposes the durable Dream request API and its workers.
// HTTP creates pending records, cancels in-flight jobs, and archives
// terminal jobs. Workers move pending → running → completed/failed.
package dreams

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	managedAgentsBeta          = "managed-agents-2026-04-01"
	dreamingBeta               = "dreaming-2026-04-21"
	defaultAnthropicAPIVersion = "2023-06-01"
	maxDreamBodySize           = 1 << 20
	maxInstructionBytes        = 4096
	dreamInternalSessionKind   = "dream"
)

type Handler struct {
	db           *db.DB
	codeSessions *codesessions.Service
	logger       *slog.Logger
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}
type createRequest struct {
	Model        json.RawMessage     `json:"model"`
	Instructions *string             `json:"instructions"`
	Inputs       []dreamInputRequest `json:"inputs"`
}
type dreamResponse struct {
	Type         string              `json:"type"`
	ID           string              `json:"id"`
	Status       string              `json:"status"`
	Inputs       json.RawMessage     `json:"inputs"`
	Outputs      json.RawMessage     `json:"outputs"`
	Model        dreamModelResponse  `json:"model"`
	Instructions *string             `json:"instructions"`
	SessionID    *string             `json:"session_id"`
	CreatedAt    string              `json:"created_at"`
	UpdatedAt    string              `json:"updated_at"`
	EndedAt      *string             `json:"ended_at"`
	ArchivedAt   *string             `json:"archived_at"`
	Usage        dreamUsageResponse  `json:"usage"`
	Error        *dreamErrorResponse `json:"error"`
}

// dreamUsageResponse mirrors the Anthropic Dream usage object. RunningWorker
// persists the four public counts from Session events; extra runtime fields
// are dropped at this boundary.
type dreamUsageResponse struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// dreamErrorResponse is the terminal failure the worker persisted. Type is a
// stable machine-readable category; Message is the diagnostic detail.
type dreamErrorResponse struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
type pageResponse struct {
	Data     []dreamResponse `json:"data"`
	NextPage *string         `json:"next_page"`
}
type cursorPayload struct {
	CreatedAt string `json:"created_at"`
	UUID      string `json:"uuid"`
}

func NewHandler(database *db.DB, logger *slog.Logger) *Handler {
	h := &Handler{db: database, logger: logging.LoggerOrDefault(logger), errorAdapter: httpapi.NewErrorAdapter(logger)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.routeNotFound))
	router.MethodNotAllowed(wrap(h.routeNotFound))
	router.Post("/", wrap(h.create))
	router.Get("/", wrap(h.list))
	router.Get("/{dream_id}", wrap(h.retrieve))
	router.Post("/{dream_id}/cancel", wrap(h.cancel))
	router.Post("/{dream_id}/archive", wrap(h.archive))
	h.router = router
	return h
}

func (h *Handler) WithCodeSessions(service *codesessions.Service) *Handler {
	if h != nil {
		h.codeSessions = service
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !contractEnabled(r) {
		h.errorAdapter.Write(w, r, contractRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}
func (h *Handler) routeNotFound(http.ResponseWriter, *http.Request) error { return notFound() }

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireWorkspaceCredential(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createRequest](w, r, maxDreamBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	model, err := parseDreamModel(body.Model)
	if err != nil {
		return invalidRequest(err)
	}
	if err := h.validateConfiguredModel(r, principal, model); err != nil {
		return err
	}
	if body.Instructions != nil && utf8.RuneCountInString(*body.Instructions) > maxInstructionBytes {
		return invalidRequest(fmt.Errorf("instructions must be at most %d characters", maxInstructionBytes))
	}
	inputs, err := h.normalizeInputs(r, principal, body.Inputs)
	if err != nil {
		return err
	}
	id, err := ids.New("drm_")
	if err != nil {
		return internalError("Could not generate Dream ID", err)
	}
	now := time.Now().UTC()
	created, err := h.db.CreateDream(r.Context(), db.Dream{UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID, CreatedByAPIKeyUUID: principal.APIKeyUUID, RuntimeUserUUID: principal.UserUUID, Status: "pending", Model: model, Instructions: body.Instructions, Inputs: inputs, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return internalError("Could not create Dream", fmt.Errorf("create dream %q: %w", id, err))
	}
	httpapi.WriteJSON(w, http.StatusCreated, responseFromDream(created))
	return nil
}

func (h *Handler) normalizeInputs(r *http.Request, principal auth.Principal, inputs []dreamInputRequest) (json.RawMessage, error) {
	parsed, err := parseRequestedDreamInputs(inputs)
	if err != nil {
		return nil, invalidRequest(err)
	}
	parsed.MemoryStoreID = strings.TrimSpace(parsed.MemoryStoreID)
	if parsed.MemoryStoreID == "" {
		return nil, invalidRequest(errors.New("memory_store.memory_store_id is required"))
	}
	store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceUUID, parsed.MemoryStoreID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, invalidRequest(fmt.Errorf("Memory store not found: %s", parsed.MemoryStoreID))
		}
		return nil, internalError("Could not validate memory store", err)
	}
	if store.ArchivedAt != nil {
		return nil, invalidRequest(errors.New("memory store must not be archived"))
	}
	if err := dreamSessionCountError(len(parsed.SessionIDs)); err != nil {
		return nil, invalidRequest(err)
	}
	seen := make(map[string]struct{}, len(parsed.SessionIDs))
	sessions := make([]string, 0, len(parsed.SessionIDs))
	for _, id := range parsed.SessionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, invalidRequest(errors.New("sessions.session_ids must not contain an empty value"))
		}
		if _, ok := seen[id]; ok {
			return nil, invalidRequest(fmt.Errorf("duplicate session id: %s", id))
		}
		seen[id] = struct{}{}
		session, found, lookupErr := h.db.GetSession(r.Context(), principal.WorkspaceUUID, id)
		if lookupErr != nil {
			return nil, internalError("Could not validate session", lookupErr)
		}
		if !found {
			return nil, invalidRequest(fmt.Errorf("Session not found: %s", id))
		}
		if err := reviewSessionError(session); err != nil {
			return nil, err
		}
		sessions = append(sessions, id)
	}
	encoded, err := marshalOfficialDreamInputs(dreamInputSelection{MemoryStoreID: parsed.MemoryStoreID, SessionIDs: sessions})
	if err != nil {
		return nil, internalError("Could not encode Dream inputs", err)
	}
	return encoded, nil
}

func (h *Handler) validateConfiguredModel(r *http.Request, principal auth.Principal, model string) error {
	models, err := llmproviders.ListModelIDs(r.Context(), h.db, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		if errors.Is(err, llmproviders.ErrNotConfigured) || errors.Is(err, llmproviders.ErrAmbiguousModel) {
			return invalidRequest(errors.New("model is not configured for this workspace"))
		}
		return internalError("Could not validate model", err)
	}
	if !configuredModel(models, model) {
		return invalidRequest(fmt.Errorf("model %q is not configured for this workspace", model))
	}
	return nil
}

func configuredModel(models []string, model string) bool {
	for _, configured := range models {
		if configured == model {
			return true
		}
	}
	return false
}

func sessionIsDreamInternal(session db.Session) bool {
	var metadata struct {
		InternalKind string `json:"internal_kind"`
	}
	if json.Unmarshal(session.Metadata, &metadata) == nil && metadata.InternalKind == dreamInternalSessionKind {
		return true
	}
	if session.Title == nil {
		return false
	}
	return strings.HasPrefix(*session.Title, "Dream drm_") || strings.HasPrefix(*session.Title, "Dream preparation drm_")
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireWorkspaceCredential(r)
	if err != nil {
		return err
	}
	record, err := h.db.GetDream(r.Context(), principal.WorkspaceUUID, chi.URLParam(r, "dream_id"))
	if err != nil {
		return dreamLoadError(err, chi.URLParam(r, "dream_id"))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record))
	return nil
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireWorkspaceCredential(r)
	if err != nil {
		return err
	}
	id := chi.URLParam(r, "dream_id")
	record, err := h.db.GetDream(r.Context(), principal.WorkspaceUUID, id)
	if err != nil {
		return dreamLoadError(err, id)
	}
	if record.Status == "canceled" {
		httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record))
		return nil
	}
	if err := cancelDreamStatusError(record.Status); err != nil {
		return err
	}
	var session *db.Session
	var interrupt *db.SessionEvent
	if output, outputErr := dreamOutput(record.Outputs); outputErr == nil {
		loaded, found, loadErr := h.db.GetSession(r.Context(), record.WorkspaceUUID, output.InternalSessionID)
		if loadErr != nil {
			return internalError("Could not cancel Dream", fmt.Errorf("load internal Dream session: %w", loadErr))
		}
		if found && loaded.ArchivedAt == nil {
			event, eventErr := dreamInterruptEvent(record, loaded, time.Now().UTC())
			if eventErr != nil {
				return internalError("Could not cancel Dream", eventErr)
			}
			session = &loaded
			interrupt = &event
		}
	}
	canceled, persistedInterrupt, won, err := h.db.CancelDream(r.Context(), record, session, interrupt, time.Now().UTC())
	if err != nil {
		return dreamLoadError(err, id)
	}
	if !won {
		record, err = h.db.GetDream(r.Context(), principal.WorkspaceUUID, id)
		if err != nil {
			return dreamLoadError(err, id)
		}
		if record.Status == "canceled" {
			httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record))
			return nil
		}
		if err := cancelDreamStatusError(record.Status); err != nil {
			return err
		}
		return internalError("Could not cancel Dream", errors.New("Dream cancel lost the terminal CAS"))
	}
	if persistedInterrupt != nil && session != nil && h.codeSessions != nil {
		if interruptErr := h.codeSessions.QueuePublicSessionEvents(r.Context(), *session, []db.SessionEvent{*persistedInterrupt}); interruptErr != nil {
			h.logger.WarnContext(r.Context(), "defer canceled dream interrupt", "dream_id", canceled.ExternalID, "error", interruptErr)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(canceled))
	return nil
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireWorkspaceCredential(r)
	if err != nil {
		return err
	}
	id := chi.URLParam(r, "dream_id")
	record, err := h.db.GetDream(r.Context(), principal.WorkspaceUUID, id)
	if err != nil {
		return dreamLoadError(err, id)
	}
	if err := archiveDreamStatusError(record.Status); err != nil {
		return err
	}
	record, err = h.db.ArchiveDream(r.Context(), principal.WorkspaceUUID, id)
	if err != nil {
		return dreamLoadError(err, id)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDream(record))
	return nil
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireWorkspaceCredential(r)
	if err != nil {
		return err
	}
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	records, hasMore, err := h.db.ListDreamsPage(r.Context(), db.ListDreamsPageParams{WorkspaceUUID: principal.WorkspaceUUID, Limit: limit, Cursor: cursor, IncludeArchived: includeArchived})
	if err != nil {
		return internalError("Could not list Dreams", err)
	}
	data := make([]dreamResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromDream(record))
	}
	var next *string
	if hasMore && len(records) > 0 {
		value := encodeCursor(records[len(records)-1])
		next = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: next})
	return nil
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

// usageResponseFromDream tolerates the empty `{}` default and any extra
// runtime fields; unknown or malformed usage renders as zero counts.
func usageResponseFromDream(raw json.RawMessage) dreamUsageResponse {
	var usage dreamUsageResponse
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &usage)
	}
	return usage
}

// errorResponseFromDream returns nil for JSON null, the legacy `[]` default,
// and any payload without a type, so a Dream that has not failed shows
// `"error": null` rather than an empty object.
func errorResponseFromDream(raw json.RawMessage) *dreamErrorResponse {
	if len(raw) == 0 {
		return nil
	}
	var value dreamErrorResponse
	if err := json.Unmarshal(raw, &value); err != nil || value.Type == "" {
		return nil
	}
	return &value
}
func requireWorkspaceCredential(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || (principal.CredentialType != auth.CredentialTypeAPIKey && principal.CredentialType != auth.CredentialTypePlatformSession) {
		return auth.Principal{}, authenticationRequired()
	}
	return principal, nil
}
func contractEnabled(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("anthropic-version")) != defaultAnthropicAPIVersion {
		return false
	}
	required := map[string]bool{managedAgentsBeta: false, dreamingBeta: false}
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, beta := range strings.Split(value, ",") {
			if _, ok := required[strings.TrimSpace(beta)]; ok {
				required[strings.TrimSpace(beta)] = true
			}
		}
	}
	return required[managedAgentsBeta] && required[dreamingBeta]
}
func encodeCursor(value db.Dream) string {
	payload, _ := json.Marshal(cursorPayload{CreatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano), UUID: value.UUID})
	return base64.RawURLEncoding.EncodeToString(payload)
}
func decodeCursor(raw string) (*db.DreamPageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil || payload.UUID == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.DreamPageCursor{CreatedAt: createdAt, UUID: payload.UUID}, nil
}
