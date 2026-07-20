package batches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const messageBatchesBeta = "message-batches-2024-09-24"

var customIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Handler struct {
	cfg    config.Config
	db     *db.DB
	store  storage.ObjectStore
	router chi.Router
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

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore) *Handler {
	h := &Handler{cfg: cfg, db: database, store: store}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.createRoute)
	router.Get("/", h.list)
	router.Get("/{message_batch_id}", h.retrieveRoute)
	router.Delete("/{message_batch_id}", h.deleteRoute)
	router.Post("/{message_batch_id}/cancel", h.cancelRoute)
	router.Get("/{message_batch_id}/results", h.resultsRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isBeta, betaHeaders, betaErr := parseBatchBeta(r)
	if betaErr != nil {
		httpapi.WriteError(w, r, betaErr)
		return
	}

	r = r.WithContext(withBatchBetaInfo(r.Context(), batchBetaInfo{IsBeta: isBeta, BetaHeaders: betaHeaders}))
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func withBatchBetaInfo(ctx context.Context, info batchBetaInfo) context.Context {
	return context.WithValue(ctx, batchBetaContextKey{}, info)
}

func batchBetaInfoFromContext(ctx context.Context) batchBetaInfo {
	info, _ := ctx.Value(batchBetaContextKey{}).(batchBetaInfo)
	return info
}

func (h *Handler) createRoute(w http.ResponseWriter, r *http.Request) {
	info := batchBetaInfoFromContext(r.Context())
	h.create(w, r, info.IsBeta, info.BetaHeaders)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, isBeta bool, betaHeaders []string) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}
	if h.isOfficialSDKFixture(principal) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureBatchResponse(r, h.cfg.SDKFixtures.BatchID, "in_progress"))
		return
	}
	if h.cfg.AnthropicUpstream.APIKey == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, "api_error", "anthropic_upstream.api_key is required for Message Batches"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.Batch.MaxBodyBytes)
	var body createRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		status := http.StatusBadRequest
		message := "Invalid JSON body"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
			message = "Request body exceeds maximum size"
		}
		httpapi.WriteError(w, r, httpapi.NewError(status, "invalid_request_error", message))
		return
	}
	if err := h.validateCreate(body, betaHeaders); err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}

	externalID, err := ids.New("msgbatch_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate batch ID"))
		return
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
		UUID:              uuid.NewString(),
		ExternalID:        externalID,
		WorkspaceID:       principal.WorkspaceID,
		WorkspaceUUID:     principal.WorkspaceUUID,
		CreatedByAPIKeyID: principal.APIKeyID,
		APIVariant:        apiVariant,
		AnthropicVersion:  anthropicVersion,
		BetaHeaders:       betaHeaders,
		CreatedAt:         now,
		ExpiresAt:         now.Add(24 * time.Hour),
	}
	reqs := make([]db.NewBatchRequest, 0, len(body.Requests))
	for i, item := range body.Requests {
		reqID, err := ids.New("msgbatchreq_")
		if err != nil {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate batch request ID"))
			return
		}
		reqs = append(reqs, db.NewBatchRequest{
			ExternalID:   reqID,
			WorkspaceID:  principal.WorkspaceID,
			RequestIndex: i,
			CustomID:     item.CustomID,
			Params:       item.Params,
		})
	}
	created, err := h.db.CreateMessageBatch(r.Context(), record, reqs)
	if err != nil {
		log.Printf("create message batch: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create message batch"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromRecord(r, created))
}

func (h *Handler) validateCreate(body createRequest, betaHeaders []string) error {
	if len(body.Requests) == 0 {
		return errors.New("requests must contain at least one request")
	}
	if h.cfg.Batch.MaxRequests > 0 && len(body.Requests) > h.cfg.Batch.MaxRequests {
		return fmt.Errorf("requests must contain at most %d requests", h.cfg.Batch.MaxRequests)
	}
	for _, beta := range betaHeaders {
		if beta == "output-300k-2026-03-24" {
			return errors.New("output-300k-2026-03-24 is not supported in Local Fan-out Message Batches")
		}
	}
	seen := make(map[string]struct{}, len(body.Requests))
	for _, item := range body.Requests {
		if !customIDPattern.MatchString(item.CustomID) {
			return errors.New("custom_id must match ^[A-Za-z0-9_-]{1,64}$")
		}
		if _, ok := seen[item.CustomID]; ok {
			return errors.New("custom_id must be unique within a batch")
		}
		seen[item.CustomID] = struct{}{}
		if !isJSONObject(item.Params) {
			return errors.New("params must be a JSON object")
		}
		if err := validateParams(item.Params); err != nil {
			return fmt.Errorf("params for custom_id %s: %w", item.CustomID, err)
		}
	}
	return nil
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

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixture(principal) {
		batch := h.fixtureBatchResponse(r, h.cfg.SDKFixtures.BatchID, "in_progress")
		first := batch.ID
		httpapi.WriteJSON(w, http.StatusOK, listResponse{Data: []messageBatchResponse{batch}, FirstID: &first, LastID: &first})
		return
	}
	limit, err := parseLimit(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	records, hasMore, err := h.db.ListMessageBatchesPage(r.Context(), db.ListMessageBatchesPageParams{
		WorkspaceID: principal.WorkspaceID,
		AfterID:     r.URL.Query().Get("after_id"),
		BeforeID:    r.URL.Query().Get("before_id"),
		Limit:       limit,
	})
	if err != nil {
		log.Printf("list message batches: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list message batches"))
		return
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
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieve(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) cancelRoute(w http.ResponseWriter, r *http.Request) {
	h.cancel(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) resultsRoute(w http.ResponseWriter, r *http.Request) {
	h.results(w, r, chi.URLParam(r, "message_batch_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, batchID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.db.GetMessageBatch(r.Context(), principal.WorkspaceID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureID(principal, batchID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureBatchResponse(r, batchID, "ended"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Message batch not found: "+batchID))
			return
		}
		log.Printf("get message batch: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve message batch"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromRecord(r, record))
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request, batchID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, batchID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureBatchResponse(r, batchID, "canceling"))
		return
	}
	record, err := h.db.CancelMessageBatch(r.Context(), principal.WorkspaceID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Message batch not found: "+batchID))
			return
		}
		log.Printf("cancel message batch: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not cancel message batch"))
		return
	}
	if record.ProcessingStatus == "canceling" {
		if err := h.db.EnqueueMessageBatchJob(r.Context(), record.WorkspaceID, record.ID, record.ExternalID); err != nil {
			log.Printf("enqueue cancel message batch job batch_id=%s: %v", record.ExternalID, err)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromRecord(r, record))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, batchID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, batchID) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": batchID, "type": "message_batch_deleted"})
		return
	}
	record, err := h.db.GetMessageBatch(r.Context(), principal.WorkspaceID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Message batch not found: "+batchID))
			return
		}
		log.Printf("get message batch before delete: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete message batch"))
		return
	}
	if err := h.db.SoftDeleteMessageBatch(r.Context(), principal.WorkspaceID, batchID); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "invalid_request_error", "Message batch must be ended before deletion"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Message batch not found: "+batchID))
			return
		}
		log.Printf("soft delete message batch: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete message batch"))
		return
	}
	if record.ResultsS3Key != nil {
		if err := h.store.Delete(r.Context(), *record.ResultsS3Key); err != nil {
			log.Printf("delete message batch results after soft delete batch_id=%s key=%s: %v", batchID, *record.ResultsS3Key, err)
			if enqueueErr := h.db.EnqueueObjectCleanupJob(r.Context(), record.WorkspaceID, valueOrEmpty(record.ResultsS3Bucket), *record.ResultsS3Key, record.ExternalID); enqueueErr != nil {
				log.Printf("enqueue batch results cleanup batch_id=%s key=%s: %v", batchID, *record.ResultsS3Key, enqueueErr)
			}
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": batchID, "type": "message_batch_deleted"})
}

func (h *Handler) results(w http.ResponseWriter, r *http.Request, batchID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, batchID) {
		w.Header().Set("Content-Type", "application/x-jsonl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"custom_id":"req_1","result":{"type":"succeeded","message":null}}` + "\n"))
		return
	}
	record, err := h.db.GetMessageBatch(r.Context(), principal.WorkspaceID, batchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Message batch not found: "+batchID))
			return
		}
		log.Printf("get message batch before results: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve message batch results"))
		return
	}
	if record.ProcessingStatus != "ended" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Message batch has not ended"))
		return
	}
	if resultsExpired(record, h.cfg.Batch.ResultRetentionDays) || record.ResultsS3Key == nil || record.ResultsSizeBytes == nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Message batch results are not available"))
		return
	}
	object, err := h.store.Get(r.Context(), *record.ResultsS3Key)
	if err != nil {
		log.Printf("get message batch results object batch_id=%s key=%s: %v", batchID, *record.ResultsS3Key, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve message batch results"))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", "application/x-jsonl")
	w.Header().Set("Content-Length", strconv.FormatInt(*record.ResultsSizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		log.Printf("download batch results stream failed batch_id=%s key=%s bytes_copied=%d expected_size=%d: %v", batchID, *record.ResultsS3Key, copied, *record.ResultsSizeBytes, copyErr)
		return
	}
	if copied != *record.ResultsSizeBytes {
		log.Printf("download batch results size mismatch batch_id=%s key=%s bytes_copied=%d expected_size=%d", batchID, *record.ResultsS3Key, copied, *record.ResultsSizeBytes)
	}
}

func parseBatchBeta(r *http.Request) (bool, []string, *httpapi.Error) {
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
		return true, nil, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Message Batches beta requires anthropic-beta: message-batches-2024-09-24")
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
