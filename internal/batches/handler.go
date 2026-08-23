package batches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
)

const messageBatchesBeta = "message-batches-2024-09-24"

var customIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Handler struct {
	cfg          config.Config
	db           *db.DB
	store        storage.ObjectStore
	logger       *slog.Logger
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

type batchBetaInfo struct {
	IsBeta      bool
	BetaHeaders []string
}

type batchBetaContextKey struct{}

type createRequest struct {
	Requests []createBatchRequest `json:"requests"`
}

type createBatchRequest struct {
	CustomID string          `json:"custom_id"`
	Params   json.RawMessage `json:"params"`
}

type messageBatchResponse struct {
	ID                string        `json:"id"`
	Type              string        `json:"type"`
	ProcessingStatus  string        `json:"processing_status"`
	RequestCounts     requestCounts `json:"request_counts"`
	CreatedAt         string        `json:"created_at"`
	ExpiresAt         string        `json:"expires_at"`
	EndedAt           *string       `json:"ended_at"`
	CancelInitiatedAt *string       `json:"cancel_initiated_at"`
	ArchivedAt        *string       `json:"archived_at"`
	ResultsURL        *string       `json:"results_url"`
}

type requestCounts struct {
	Processing int `json:"processing"`
	Succeeded  int `json:"succeeded"`
	Errored    int `json:"errored"`
	Canceled   int `json:"canceled"`
	Expired    int `json:"expired"`
}

type listResponse struct {
	Data    []messageBatchResponse `json:"data"`
	HasMore bool                   `json:"has_more"`
	FirstID *string                `json:"first_id"`
	LastID  *string                `json:"last_id"`
}

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{cfg: cfg, db: database, store: store, logger: logger, errorAdapter: httpapi.NewErrorAdapter(logger)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Post("/", wrap(h.createRoute))
	router.Get("/", wrap(h.list))
	router.Get("/{message_batch_id}", wrap(h.retrieveRoute))
	router.Delete("/{message_batch_id}", wrap(h.deleteRoute))
	router.Post("/{message_batch_id}/cancel", wrap(h.cancelRoute))
	router.Get("/{message_batch_id}/results", h.resultsRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isBeta, betaHeaders, betaErr := parseBatchBeta(r)
	if betaErr != nil {
		h.errorAdapter.Write(w, r, betaErr)
		return
	}

	r = r.WithContext(withBatchBetaInfo(r.Context(), batchBetaInfo{IsBeta: isBeta, BetaHeaders: betaHeaders}))
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return batchRouteNotFound()
}

func withBatchBetaInfo(ctx context.Context, info batchBetaInfo) context.Context {
	return context.WithValue(ctx, batchBetaContextKey{}, info)
}

func batchBetaInfoFromContext(ctx context.Context) batchBetaInfo {
	info, _ := ctx.Value(batchBetaContextKey{}).(batchBetaInfo)
	return info
}

func (h *Handler) createRoute(w http.ResponseWriter, r *http.Request) error {
	info := batchBetaInfoFromContext(r.Context())
	return h.create(w, r, info.IsBeta, info.BetaHeaders)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, isBeta bool, betaHeaders []string) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return batchAuthenticationRequired()
	}
	if h.isOfficialSDKFixture(principal) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureBatchResponse(r, h.cfg.SDKFixtures.BatchID, "in_progress"))
		return nil
	}
	body, err := httpapi.DecodeObjectBodyAs[createRequest](w, r, h.cfg.Batch.MaxBodyBytes)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return requestTooLarge(err)
		}
		return invalidRequest(err)
	}
	requestModels, err := h.validateCreate(body, betaHeaders)
	if err != nil {
		return invalidRequest(err)
	}
	if err := h.validateConfiguredModels(
		r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, body, requestModels,
	); err != nil {
		return err
	}

	externalID, err := ids.New("msgbatch_")
	if err != nil {
		return internalError("Could not generate batch ID", fmt.Errorf("generate message batch ID: %w", err))
	}
	now := time.Now().UTC()
	apiVariant := "stable"
	if isBeta {
		apiVariant = "beta"
	}
	anthropicVersion := strings.TrimSpace(r.Header.Get("anthropic-version"))
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	record := db.MessageBatch{
		UUID:                uuid.NewV4().String(),
		ExternalID:          externalID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		APIVariant:          apiVariant,
		AnthropicVersion:    anthropicVersion,
		BetaHeaders:         betaHeaders,
		CreatedAt:           now,
		ExpiresAt:           now.Add(24 * time.Hour),
	}
	reqs := make([]db.NewBatchRequest, 0, len(body.Requests))
	for i, item := range body.Requests {
		reqID, err := ids.New("msgbatchreq_")
		if err != nil {
			return internalError("Could not generate batch request ID", fmt.Errorf("generate message batch request ID at index %d: %w", i, err))
		}
		reqs = append(reqs, db.NewBatchRequest{
			ExternalID:    reqID,
			WorkspaceUUID: principal.WorkspaceUUID,
			RequestIndex:  i,
			CustomID:      item.CustomID,
			Params:        item.Params,
		})
	}
	created, err := h.db.CreateMessageBatch(r.Context(), record, reqs)
	if err != nil {
		return internalError("Could not create message batch", fmt.Errorf("create message batch %q: %w", externalID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromRecord(r, created))
	return nil
}

func (h *Handler) validateConfiguredModels(
	ctx context.Context,
	organizationUUID, workspaceUUID string,
	body *createRequest,
	requestModels []string,
) error {
	modelIDs, err := llmproviders.ListModelIDs(ctx, h.db, organizationUUID, workspaceUUID)
	if err != nil {
		return configuredModelError(err)
	}
	configured := make(map[string]struct{}, len(modelIDs))
	for _, modelID := range modelIDs {
		configured[modelID] = struct{}{}
	}
	for index, modelID := range requestModels {
		if _, ok := configured[modelID]; !ok {
			return invalidRequest(fmt.Errorf(
				"params for custom_id %s: model %q is not configured for this workspace",
				body.Requests[index].CustomID,
				modelID,
			))
		}
	}
	return nil
}

func (h *Handler) validateCreate(body *createRequest, betaHeaders []string) ([]string, error) {
	if len(body.Requests) == 0 {
		return nil, errors.New("requests must contain at least one request")
	}
	if h.cfg.Batch.MaxRequests > 0 && len(body.Requests) > h.cfg.Batch.MaxRequests {
		return nil, fmt.Errorf("requests must contain at most %d requests", h.cfg.Batch.MaxRequests)
	}
	for _, beta := range betaHeaders {
		if beta == "output-300k-2026-03-24" {
			return nil, errors.New("output-300k-2026-03-24 is not supported in Local Fan-out Message Batches")
		}
	}
	seen := make(map[string]struct{}, len(body.Requests))
	requestModels := make([]string, 0, len(body.Requests))
	for _, item := range body.Requests {
		if !customIDPattern.MatchString(item.CustomID) {
			return nil, errors.New("custom_id must match ^[A-Za-z0-9_-]{1,64}$")
		}
		if _, ok := seen[item.CustomID]; ok {
			return nil, errors.New("custom_id must be unique within a batch")
		}
		seen[item.CustomID] = struct{}{}
		if !isJSONObject(item.Params) {
			return nil, errors.New("params must be a JSON object")
		}
		if err := validateParams(item.Params); err != nil {
			return nil, fmt.Errorf("params for custom_id %s: %w", item.CustomID, err)
		}
		modelID, err := llmproviders.MessageRequestModel(item.Params)
		if err != nil {
			return nil, fmt.Errorf("params for custom_id %s: %w", item.CustomID, err)
		}
		requestModels = append(requestModels, modelID)
	}
	return requestModels, nil
}

func isJSONObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil
}

func validateParams(raw json.RawMessage) error {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		return errors.New("params must be a JSON object")
	}
	if rawMax, ok := params["max_tokens"]; ok {
		var maxTokens int64
		if err := json.Unmarshal(rawMax, &maxTokens); err != nil || maxTokens < 1 {
			return errors.New("max_tokens must be greater than or equal to 1")
		}
	}
	if rawStream, ok := params["stream"]; ok {
		var stream bool
		if json.Unmarshal(rawStream, &stream) == nil && stream {
			return errors.New("stream: true is not supported")
		}
	}
	for _, field := range []string{"speed", "store", "previous_thread_event_id", "cache_hint", "context_hint"} {
		if _, ok := params[field]; ok {
			return fmt.Errorf("%s is not supported", field)
		}
	}
	if rawResearch, ok := params["research_preview_2026_02"]; ok {
		var value string
		if json.Unmarshal(rawResearch, &value) == nil && value == "active" {
			return errors.New("research_preview_2026_02 active is not supported")
		}
	}
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixture(principal) {
		batch := h.fixtureBatchResponse(r, h.cfg.SDKFixtures.BatchID, "in_progress")
		first := batch.ID
		httpapi.WriteJSON(w, http.StatusOK, listResponse{Data: []messageBatchResponse{batch}, FirstID: &first, LastID: &first})
		return nil
	}
	limit, err := parseLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListMessageBatchesPage(r.Context(), db.ListMessageBatchesPageParams{
		WorkspaceUUID: principal.WorkspaceUUID,
		AfterID:       r.URL.Query().Get("after_id"),
		BeforeID:      r.URL.Query().Get("before_id"),
		Limit:         limit,
	})
	if err != nil {
		return internalError("Could not list message batches", fmt.Errorf("list message batches: %w", err))
	}
	data := make([]messageBatchResponse, 0, len(records))
	for _, record := range records {
		data = append(data, h.responseFromRecord(r, record))
	}
	var firstID, lastID *string
	if len(data) > 0 {
		firstID = &data[0].ID
		lastID = &data[len(data)-1].ID
	}
	httpapi.WriteJSON(w, http.StatusOK, listResponse{Data: data, HasMore: hasMore, FirstID: firstID, LastID: lastID})
	return nil
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) error {
	return h.retrieve(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) cancelRoute(w http.ResponseWriter, r *http.Request) error {
	return h.cancel(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) error {
	return h.delete(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) resultsRoute(w http.ResponseWriter, r *http.Request) {
	h.results(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, batchID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.db.GetMessageBatch(r.Context(), principal.WorkspaceUUID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureID(principal, batchID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureBatchResponse(r, batchID, "ended"))
			return nil
		}
		if errors.Is(err, db.ErrNotFound) {
			return messageBatchNotFound(batchID, err)
		}
		return internalError("Could not retrieve message batch", fmt.Errorf("retrieve message batch %q: %w", batchID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromRecord(r, record))
	return nil
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request, batchID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, batchID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureBatchResponse(r, batchID, "canceling"))
		return nil
	}
	record, err := h.db.CancelMessageBatch(r.Context(), principal.WorkspaceUUID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return messageBatchNotFound(batchID, err)
		}
		return internalError("Could not cancel message batch", fmt.Errorf("cancel message batch %q: %w", batchID, err))
	}
	if record.ProcessingStatus == "canceling" {
		if err := h.db.EnqueueMessageBatchJob(r.Context(), record.WorkspaceUUID, record.UUID, record.ExternalID); err != nil {
			h.logger.ErrorContext(r.Context(), "enqueue cancel message batch job", "batch_id", record.ExternalID, "error", err)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromRecord(r, record))
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, batchID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, batchID) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": batchID, "type": "message_batch_deleted"})
		return nil
	}
	record, err := h.db.GetMessageBatch(r.Context(), principal.WorkspaceUUID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return messageBatchNotFound(batchID, err)
		}
		return internalError("Could not delete message batch", fmt.Errorf("retrieve message batch %q for deletion: %w", batchID, err))
	}
	if err := h.db.SoftDeleteMessageBatch(r.Context(), principal.WorkspaceUUID, batchID); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return messageBatchMustBeEnded(err)
		}
		if errors.Is(err, db.ErrNotFound) {
			return messageBatchNotFound(batchID, err)
		}
		return internalError("Could not delete message batch", fmt.Errorf("soft delete message batch %q: %w", batchID, err))
	}
	if record.ResultsS3Key != nil {
		if err := h.store.Delete(r.Context(), *record.ResultsS3Key, storage.DeleteOptions{}); err != nil {
			h.logger.ErrorContext(r.Context(), "delete message batch results after soft delete", "batch_id", batchID, "key", *record.ResultsS3Key, "error", err)
			if enqueueErr := h.db.EnqueueObjectCleanupJob(r.Context(), record.WorkspaceUUID, valueOrEmpty(record.ResultsS3Bucket), *record.ResultsS3Key, record.ExternalID); enqueueErr != nil {
				h.logger.ErrorContext(r.Context(), "enqueue batch results cleanup", "batch_id", batchID, "key", *record.ResultsS3Key, "error", enqueueErr)
			}
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": batchID, "type": "message_batch_deleted"})
	return nil
}

func (h *Handler) results(w http.ResponseWriter, r *http.Request, batchID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, batchID) {
		w.Header().Set("Content-Type", "application/x-jsonl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"custom_id":"req_1","result":{"type":"succeeded","message":null}}` + "\n"))
		return
	}
	record, err := h.db.GetMessageBatch(r.Context(), principal.WorkspaceUUID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.errorAdapter.Write(w, r, messageBatchNotFound(batchID, err))
			return
		}
		h.errorAdapter.Write(w, r, internalError("Could not retrieve message batch results", fmt.Errorf("get message batch %q before results: %w", batchID, err)))
		return
	}
	if record.ProcessingStatus != "ended" {
		h.errorAdapter.Write(w, r, messageBatchHasNotEnded())
		return
	}
	if resultsExpired(record, h.cfg.Batch.ResultRetentionDays) || record.ResultsS3Key == nil || record.ResultsSizeBytes == nil {
		h.errorAdapter.Write(w, r, messageBatchResultsUnavailable())
		return
	}
	object, err := h.store.Open(r.Context(), *record.ResultsS3Key, nil)
	if err != nil {
		h.errorAdapter.Write(w, r, internalError("Could not retrieve message batch results", fmt.Errorf("open message batch %q results object %q: %w", batchID, *record.ResultsS3Key, err)))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", "application/x-jsonl")
	w.Header().Set("Content-Length", strconv.FormatInt(*record.ResultsSizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		h.logger.ErrorContext(r.Context(), "download batch results stream failed", "batch_id", batchID, "key", *record.ResultsS3Key, "bytes_copied", copied, "expected_size", *record.ResultsSizeBytes, "error", copyErr)
		return
	}
	if copied != *record.ResultsSizeBytes {
		h.logger.WarnContext(r.Context(), "download batch results size mismatch", "batch_id", batchID, "key", *record.ResultsS3Key, "bytes_copied", copied, "expected_size", *record.ResultsSizeBytes)
	}
}

func parseBatchBeta(r *http.Request) (bool, []string, error) {
	if r.URL.Query().Get("beta") != "true" {
		return false, nil, nil
	}
	values := splitBetaHeaderValues(r.Header.Values("anthropic-beta"))
	found := false
	extras := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		if value == messageBatchesBeta {
			found = true
			continue
		}
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		extras = append(extras, value)
	}
	if !found {
		return true, nil, batchBetaRequired()
	}
	return true, extras, nil
}

func splitBetaHeaderValues(values []string) []string {
	var parts []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return parts
}

func parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 1000 {
		return 0, errors.New("limit must be between 1 and 1000")
	}
	return limit, nil
}

func (h *Handler) responseFromRecord(r *http.Request, record db.MessageBatch) messageBatchResponse {
	counts := requestCounts{Processing: record.RequestCount}
	if record.ProcessingStatus == "ended" {
		counts = requestCounts{
			Processing: record.ProcessingCount,
			Succeeded:  record.SucceededCount,
			Errored:    record.ErroredCount,
			Canceled:   record.CanceledCount,
			Expired:    record.ExpiredCount,
		}
	}
	var resultsURL *string
	if record.ProcessingStatus == "ended" && record.ArchivedAt == nil && record.ResultsS3Key != nil && !resultsExpired(record, h.cfg.Batch.ResultRetentionDays) {
		value := strings.TrimRight(httpapi.RequestBaseURL(r), "/") + "/v1/messages/batches/" + record.ExternalID + "/results"
		resultsURL = &value
	}
	return messageBatchResponse{
		ID:                record.ExternalID,
		Type:              "message_batch",
		ProcessingStatus:  record.ProcessingStatus,
		RequestCounts:     counts,
		CreatedAt:         formatTime(record.CreatedAt),
		ExpiresAt:         formatTime(record.ExpiresAt),
		EndedAt:           formatOptionalTime(record.EndedAt),
		CancelInitiatedAt: formatOptionalTime(record.CancelInitiatedAt),
		ArchivedAt:        formatOptionalTime(record.ArchivedAt),
		ResultsURL:        resultsURL,
	}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	value := formatTime(*t)
	return &value
}

func resultsExpired(record db.MessageBatch, days int) bool {
	if days <= 0 {
		return false
	}
	return time.Since(record.CreatedAt) > time.Duration(days)*24*time.Hour
}

func (h *Handler) isOfficialSDKFixture(principal auth.Principal) bool {
	return principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID
}

func (h *Handler) isOfficialSDKFixtureID(principal auth.Principal, batchID string) bool {
	return h.isOfficialSDKFixture(principal) && batchID == h.cfg.SDKFixtures.BatchID
}

func (h *Handler) fixtureBatchResponse(r *http.Request, id string, status string) messageBatchResponse {
	created := time.Unix(0, 0).UTC()
	expires := created.Add(24 * time.Hour)
	var endedAt *string
	var resultsURL *string
	counts := requestCounts{Processing: 1}
	if status == "ended" {
		endedAt = formatOptionalTime(&created)
		value := strings.TrimRight(httpapi.RequestBaseURL(r), "/") + "/v1/messages/batches/" + id + "/results"
		resultsURL = &value
		counts = requestCounts{Succeeded: 1}
	}
	return messageBatchResponse{
		ID:               id,
		Type:             "message_batch",
		ProcessingStatus: status,
		RequestCounts:    counts,
		CreatedAt:        formatTime(created),
		ExpiresAt:        formatTime(expires),
		EndedAt:          endedAt,
		ResultsURL:       resultsURL,
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
