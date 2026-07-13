package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	maxMemoryBodySize     = 1 << 20
	maxMemoryContentBytes = 102400
	maxMemoryPathBytes    = 1024
)

type Handler struct {
	cfg    config.Config
	db     *db.DB
	store  storage.ObjectStore
	router chi.Router
}

type storePageResponse struct {
	Data     []storeResponse `json:"data"`
	NextPage *string         `json:"next_page"`
}

type memoryPageResponse struct {
	Data     []any   `json:"data"`
	NextPage *string `json:"next_page"`
}

type memoryDepthPageResponse struct {
	Data     []any                  `json:"data"`
	Prefixes []memoryPrefixResponse `json:"prefixes"`
	NextPage *string                `json:"next_page"`
}

type versionPageResponse struct {
	Data     []versionResponse `json:"data"`
	NextPage *string           `json:"next_page"`
}

type storeResponse struct {
	ID          string          `json:"id"`
	CreatedAt   string          `json:"created_at"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	UpdatedAt   string          `json:"updated_at"`
	ArchivedAt  *string         `json:"archived_at"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
}

type memoryResponse struct {
	ID               string  `json:"id"`
	ContentSHA256    string  `json:"content_sha256"`
	ContentSizeBytes int64   `json:"content_size_bytes"`
	CreatedAt        string  `json:"created_at"`
	MemoryStoreID    string  `json:"memory_store_id"`
	MemoryVersionID  string  `json:"memory_version_id"`
	Path             string  `json:"path"`
	Type             string  `json:"type"`
	UpdatedAt        string  `json:"updated_at"`
	Content          *string `json:"content"`
}

type memoryPrefixResponse struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type versionResponse struct {
	ID               string         `json:"id"`
	CreatedAt        string         `json:"created_at"`
	MemoryID         string         `json:"memory_id"`
	MemoryStoreID    string         `json:"memory_store_id"`
	Operation        string         `json:"operation"`
	Type             string         `json:"type"`
	Content          *string        `json:"content"`
	ContentSHA256    *string        `json:"content_sha256"`
	ContentSizeBytes *int64         `json:"content_size_bytes"`
	CreatedBy        actorResponse  `json:"created_by"`
	Path             *string        `json:"path"`
	RedactedAt       *string        `json:"redacted_at"`
	RedactedBy       *actorResponse `json:"redacted_by"`
}

type actorResponse struct {
	Type      string `json:"type"`
	APIKeyID  string `json:"api_key_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

type memoryListItem struct {
	Path   string
	Memory *db.Memory
	Prefix *string
}

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore) *Handler {
	h := &Handler{cfg: cfg, db: database, store: store}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.createStore)
	router.Get("/", h.listStores)
	router.Get("/{memory_store_id}", h.retrieveStoreRoute)
	router.Post("/{memory_store_id}", h.updateStoreRoute)
	router.Delete("/{memory_store_id}", h.deleteStoreRoute)
	router.Post("/{memory_store_id}/archive", h.archiveStoreRoute)
	router.Post("/{memory_store_id}/memories", h.createMemoryRoute)
	router.Get("/{memory_store_id}/memories", h.listMemoriesRoute)
	router.Get("/{memory_store_id}/memories/{memory_id}", h.retrieveMemoryRoute)
	router.Post("/{memory_store_id}/memories/{memory_id}", h.updateMemoryRoute)
	router.Delete("/{memory_store_id}/memories/{memory_id}", h.deleteMemoryRoute)
	router.Get("/{memory_store_id}/memory_versions", h.listVersionsRoute)
	router.Get("/{memory_store_id}/memory_versions/{memory_version_id}", h.retrieveVersionRoute)
	router.Post("/{memory_store_id}/memory_versions/{memory_version_id}/redact", h.redactVersionRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Memory Stores API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) createStore(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	name, err := parseRequiredStringField(fields, "name")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if err := validateStoreName(name); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	description, err := optionalStringWithDefault(fields["description"], "", "description")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if err := validateDescription(description); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	metadata, err := normalizeMetadata(fieldOrDefault(fields, "metadata", `{}`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	storeID, err := ids.New("memstore_")
	if err != nil {
		writeAPIError(w, r, "Could not generate memory store ID")
		return
	}
	now := time.Now().UTC()
	created, err := h.db.CreateMemoryStore(r.Context(), db.MemoryStore{
		UUID:              uuid.NewString(),
		ExternalID:        storeID,
		OrganizationID:    principal.OrganizationID,
		WorkspaceID:       principal.WorkspaceID,
		CreatedByAPIKeyID: principal.APIKeyID,
		Name:              name,
		Description:       description,
		Metadata:          metadata,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		log.Printf("create memory store: %v", err)
		writeAPIError(w, r, "Could not create memory store")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromStore(created))
}

func (h *Handler) listStores(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, err := parseLimit(r, 100)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeStoreCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtGTE, err := parseOptionalTime(r, "created_at[gte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLTE, err := parseOptionalTime(r, "created_at[lte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListMemoryStoresPage(r.Context(), db.ListMemoryStoresPageParams{
		WorkspaceID:     principal.WorkspaceID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		log.Printf("list memory stores: %v", err)
		writeAPIError(w, r, "Could not list memory stores")
		return
	}
	data := make([]storeResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromStore(record))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeStoreCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, storePageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveStoreRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieveStore(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) retrieveStore(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.memoryStoreForRead(r.Context(), principal, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromStore(record))
}

func (h *Handler) memoryStoreForRead(ctx context.Context, principal auth.Principal, storeID string) (db.MemoryStore, error) {
	record, err := h.db.GetMemoryStore(ctx, principal.WorkspaceID, storeID)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, db.ErrNotFound) || principal.CredentialType != auth.CredentialTypePlatformSession {
		return db.MemoryStore{}, err
	}
	return h.db.GetMemoryStoreByExternalID(ctx, storeID)
}

func (h *Handler) updateStoreRoute(w http.ResponseWriter, r *http.Request) {
	h.updateStore(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) updateStore(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	current, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceID, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	if current.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("memory store must not be archived"))
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	name := current.Name
	if raw, ok := fields["name"]; ok {
		name, err = parseRequiredRawString(raw, "name")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		if err := validateStoreName(name); err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	description := current.Description
	if raw, ok := fields["description"]; ok {
		if isJSONNull(raw) {
			description = ""
		} else {
			description, err = parseRawString(raw, "description")
			if err != nil {
				writeBadRequest(w, r, err)
				return
			}
		}
		if err := validateDescription(description); err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	metadata := current.Metadata
	if raw, ok := fields["metadata"]; ok {
		metadata, err = patchMetadata(current.Metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	updated, err := h.db.UpdateMemoryStore(r.Context(), principal.WorkspaceID, storeID, db.MemoryStore{
		Name:        name,
		Description: description,
		Metadata:    metadata,
		UpdatedAt:   time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			writeBadRequest(w, r, errors.New("memory store must not be archived"))
			return
		}
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromStore(updated))
}

func (h *Handler) archiveStoreRoute(w http.ResponseWriter, r *http.Request) {
	h.archiveStore(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) archiveStore(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.db.ArchiveMemoryStore(r.Context(), principal.WorkspaceID, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromStore(record))
}

func (h *Handler) deleteStoreRoute(w http.ResponseWriter, r *http.Request) {
	h.deleteStore(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) deleteStore(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	refs, err := h.db.DeleteMemoryStore(r.Context(), principal.WorkspaceID, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	for _, ref := range refs {
		h.deleteObjectOrEnqueue(r.Context(), ref)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": storeID, "type": "memory_store_deleted"})
}

func (h *Handler) createMemoryRoute(w http.ResponseWriter, r *http.Request) {
	h.createMemory(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) createMemory(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	view, err := parseView(r, "basic")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	content, err := parseRequiredContent(fields, "content")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	path, err := parseRequiredStringField(fields, "path")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if err := validateMemoryPath(path); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceID, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	if store.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("memory store must not be archived"))
		return
	}

	memoryID, versionID, memoryUUID, versionUUID, objectKey, err := h.newMemoryObjectIDs(principal.WorkspaceUUID, store.UUID)
	if err != nil {
		writeAPIError(w, r, "Could not generate memory ID")
		return
	}
	contentBytes := []byte(content)
	contentSHA := sha256Hex(contentBytes)
	if err := h.store.Put(r.Context(), objectKey, bytes.NewReader(contentBytes), int64(len(contentBytes)), "text/plain; charset=utf-8"); err != nil {
		log.Printf("put memory content: %v", err)
		writeAPIError(w, r, "Could not store memory content")
		return
	}
	ref := db.ObjectRef{WorkspaceID: principal.WorkspaceID, Bucket: h.store.Bucket(), Key: objectKey, ResourceType: "memory_version", ResourceID: versionID}
	now := time.Now().UTC()
	record, err := h.db.CreateMemory(r.Context(), db.Memory{
		UUID:                  memoryUUID,
		ExternalID:            memoryID,
		OrganizationID:        principal.OrganizationID,
		WorkspaceID:           principal.WorkspaceID,
		MemoryStoreExternalID: storeID,
		Path:                  path,
		ContentSizeBytes:      int64(len(contentBytes)),
		ContentSHA256:         contentSHA,
		S3Bucket:              h.store.Bucket(),
		S3Key:                 objectKey,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, db.MemoryVersion{
		UUID:             versionUUID,
		ExternalID:       versionID,
		Operation:        "created",
		Path:             &path,
		ContentSizeBytes: ptrInt64(int64(len(contentBytes))),
		ContentSHA256:    &contentSHA,
		S3Bucket:         ptrString(h.store.Bucket()),
		S3Key:            &objectKey,
		CreatedBy:        apiActor(principal),
		CreatedAt:        now,
	})
	if err != nil {
		h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), ref)
		writeMemoryMutationError(w, r, err, storeID, "")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, h.responseFromMemoryWithKnownContent(record, view, content))
}

func (h *Handler) listMemoriesRoute(w http.ResponseWriter, r *http.Request) {
	h.listMemories(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	view, err := parseView(r, "basic")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	limit, err := parseLimit(r, 100)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if view == "full" && limit > 20 {
		limit = 20
	}
	order, err := parseOrder(r, "asc")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	orderBy, err := parseMemoryOrderBy(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	pathPrefix := strings.TrimSpace(r.URL.Query().Get("path_prefix"))
	if err := validatePathPrefix(pathPrefix); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	depth, err := parseOptionalDepth(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if depth != nil {
		if orderBy != "path" {
			writeBadRequest(w, r, errors.New("order_by must be path when depth is set"))
			return
		}
		store, err := h.memoryStoreForRead(r.Context(), principal, storeID)
		if err != nil {
			writeStoreLoadError(w, r, err, storeID)
			return
		}
		h.listMemoriesWithDepth(w, r, store.WorkspaceID, storeID, view, limit, order, pathPrefix, *depth)
		return
	}
	cursor, err := decodeMemoryCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	store, err := h.memoryStoreForRead(r.Context(), principal, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	records, hasMore, err := h.db.ListMemoriesPage(r.Context(), db.ListMemoriesPageParams{
		WorkspaceID:           store.WorkspaceID,
		MemoryStoreExternalID: storeID,
		Limit:                 limit,
		Cursor:                cursor,
		PathPrefix:            pathPrefix,
		Order:                 order,
		OrderBy:               orderBy,
	})
	if err != nil {
		writeMemoryLoadError(w, r, err, storeID, "")
		return
	}
	data := make([]any, 0, len(records))
	for _, record := range records {
		resp, err := h.responseFromMemory(r.Context(), record, view)
		if err != nil {
			log.Printf("read memory list content memory_id=%s key=%s: %v", record.ExternalID, record.S3Key, err)
			writeAPIError(w, r, "Could not read memory content")
			return
		}
		data = append(data, resp)
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeMemoryCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, memoryPageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) listMemoriesWithDepth(w http.ResponseWriter, r *http.Request, workspaceID int64, storeID, view string, limit int, order, pathPrefix string, depth int) {
	records, err := h.db.ListMemoriesForDepth(r.Context(), db.ListMemoriesPageParams{
		WorkspaceID:           workspaceID,
		MemoryStoreExternalID: storeID,
		PathPrefix:            pathPrefix,
	})
	if err != nil {
		writeMemoryLoadError(w, r, err, storeID, "")
		return
	}
	items := rollupMemoryItems(records, pathPrefix, depth)
	if order == "desc" {
		sort.Slice(items, func(i, j int) bool { return items[i].Path > items[j].Path })
	}
	cursor, err := decodeDepthCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if cursor != "" {
		filtered := items[:0]
		for _, item := range items {
			if (order == "asc" && item.Path > cursor) || (order == "desc" && item.Path < cursor) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]any, 0, len(items))
	prefixes := make([]memoryPrefixResponse, 0)
	for _, item := range items {
		if item.Prefix != nil {
			prefix := memoryPrefixResponse{Path: *item.Prefix, Type: "memory_prefix"}
			data = append(data, prefix)
			prefixes = append(prefixes, prefix)
			continue
		}
		resp, err := h.responseFromMemory(r.Context(), *item.Memory, view)
		if err != nil {
			log.Printf("read memory depth content memory_id=%s key=%s: %v", item.Memory.ExternalID, item.Memory.S3Key, err)
			writeAPIError(w, r, "Could not read memory content")
			return
		}
		data = append(data, resp)
	}
	var nextPage *string
	if hasMore && len(items) > 0 {
		value := encodeDepthCursor(items[len(items)-1].Path)
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, memoryDepthPageResponse{Data: data, Prefixes: prefixes, NextPage: nextPage})
}

func (h *Handler) retrieveMemoryRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieveMemory(w, r, chi.URLParam(r, "memory_store_id"), chi.URLParam(r, "memory_id"))
}

func (h *Handler) retrieveMemory(w http.ResponseWriter, r *http.Request, storeID, memoryID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	view, err := parseView(r, "full")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	store, err := h.memoryStoreForRead(r.Context(), principal, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	record, err := h.db.GetMemory(r.Context(), store.WorkspaceID, storeID, memoryID)
	if err != nil {
		writeMemoryLoadError(w, r, err, storeID, memoryID)
		return
	}
	resp, err := h.responseFromMemory(r.Context(), record, view)
	if err != nil {
		log.Printf("read memory content memory_id=%s key=%s: %v", memoryID, record.S3Key, err)
		writeAPIError(w, r, "Could not read memory content")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) updateMemoryRoute(w http.ResponseWriter, r *http.Request) {
	h.updateMemory(w, r, chi.URLParam(r, "memory_store_id"), chi.URLParam(r, "memory_id"))
}

func (h *Handler) updateMemory(w http.ResponseWriter, r *http.Request, storeID, memoryID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	view, err := parseView(r, "basic")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	content, contentPresent, err := parseOptionalContent(fields, "content")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	path, pathPresent, err := parseOptionalMemoryPath(fields, "path")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	expectedHash, err := parsePrecondition(fields["precondition"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceID, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	if store.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("memory store must not be archived"))
		return
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		record, err := h.db.GetMemory(r.Context(), principal.WorkspaceID, storeID, memoryID)
		if err != nil {
			writeMemoryLoadError(w, r, err, storeID, memoryID)
			return
		}
		targetPath := record.Path
		if pathPresent {
			targetPath = path
		}
		targetContent := ""
		targetContentKnown := false
		if contentPresent {
			targetContent = content
			targetContentKnown = true
		}
		targetSHA := record.ContentSHA256
		if contentPresent {
			targetSHA = sha256Hex([]byte(content))
		}
		if expectedHash != nil && record.ContentSHA256 != *expectedHash && record.Path == targetPath && record.ContentSHA256 == targetSHA {
			httpapi.WriteJSON(w, http.StatusOK, h.responseFromMemoryWithKnownContent(record, view, content))
			return
		}
		if record.Path == targetPath && record.ContentSHA256 == targetSHA {
			resp, err := h.responseFromMemory(r.Context(), record, view)
			if err != nil {
				log.Printf("read memory no-op content memory_id=%s key=%s: %v", memoryID, record.S3Key, err)
				writeAPIError(w, r, "Could not read memory content")
				return
			}
			httpapi.WriteJSON(w, http.StatusOK, resp)
			return
		}

		if !contentPresent {
			copied, err := h.readObjectContent(r.Context(), record.S3Key, record.ContentSizeBytes)
			if err != nil {
				log.Printf("copy memory content memory_id=%s key=%s: %v", memoryID, record.S3Key, err)
				writeAPIError(w, r, "Could not read memory content")
				return
			}
			targetContent = copied
			targetContentKnown = true
		}
		versionID, err := ids.New("memver_")
		if err != nil {
			writeAPIError(w, r, "Could not generate memory version ID")
			return
		}
		versionUUID := uuid.NewString()
		objectKey := memoryObjectKey(principal.WorkspaceUUID, store.UUID, record.UUID, versionUUID)
		contentBytes := []byte(targetContent)
		contentSHA := sha256Hex(contentBytes)
		if err := h.store.Put(r.Context(), objectKey, bytes.NewReader(contentBytes), int64(len(contentBytes)), "text/plain; charset=utf-8"); err != nil {
			log.Printf("put memory update content: %v", err)
			writeAPIError(w, r, "Could not store memory content")
			return
		}
		ref := db.ObjectRef{WorkspaceID: principal.WorkspaceID, Bucket: h.store.Bucket(), Key: objectKey, ResourceType: "memory_version", ResourceID: versionID}
		result, err := h.db.UpdateMemory(r.Context(), db.UpdateMemoryInput{
			WorkspaceID:           principal.WorkspaceID,
			MemoryStoreExternalID: storeID,
			MemoryExternalID:      memoryID,
			VersionUUID:           versionUUID,
			VersionExternalID:     versionID,
			Path:                  optionalPath(path, pathPresent),
			ContentProvided:       true,
			ContentSizeBytes:      int64(len(contentBytes)),
			ContentSHA256:         contentSHA,
			S3Bucket:              h.store.Bucket(),
			S3Key:                 objectKey,
			ExpectedContentSHA256: expectedHash,
			BaseVersionExternalID: record.CurrentVersionExternalID,
			Actor:                 apiActor(principal),
			Now:                   time.Now().UTC(),
		})
		if errors.Is(err, db.ErrVersionConflict) {
			h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), ref)
			lastErr = err
			continue
		}
		if err != nil {
			h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), ref)
			writeMemoryMutationError(w, r, err, storeID, memoryID)
			return
		}
		if !result.VersionCreated {
			h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), ref)
		}
		if targetContentKnown {
			httpapi.WriteJSON(w, http.StatusOK, h.responseFromMemoryWithKnownContent(result.Memory, view, targetContent))
			return
		}
		resp, err := h.responseFromMemory(r.Context(), result.Memory, view)
		if err != nil {
			log.Printf("read memory update response memory_id=%s key=%s: %v", memoryID, result.Memory.S3Key, err)
			writeAPIError(w, r, "Could not read memory content")
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, resp)
		return
	}
	log.Printf("update memory retry exhausted memory_id=%s: %v", memoryID, lastErr)
	writeAPIError(w, r, "Could not update memory")
}

func (h *Handler) deleteMemoryRoute(w http.ResponseWriter, r *http.Request) {
	h.deleteMemory(w, r, chi.URLParam(r, "memory_store_id"), chi.URLParam(r, "memory_id"))
}

func (h *Handler) deleteMemory(w http.ResponseWriter, r *http.Request, storeID, memoryID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	expectedHash := strings.TrimSpace(r.URL.Query().Get("expected_content_sha256"))
	var expected *string
	if expectedHash != "" {
		if !isLowerHex64(expectedHash) {
			writeBadRequest(w, r, errors.New("expected_content_sha256 must be a 64-character lowercase hex string"))
			return
		}
		expected = &expectedHash
	}
	versionID, err := ids.New("memver_")
	if err != nil {
		writeAPIError(w, r, "Could not generate memory version ID")
		return
	}
	if err := h.db.DeleteMemory(r.Context(), db.DeleteMemoryInput{
		WorkspaceID:           principal.WorkspaceID,
		MemoryStoreExternalID: storeID,
		MemoryExternalID:      memoryID,
		VersionUUID:           uuid.NewString(),
		VersionExternalID:     versionID,
		ExpectedContentSHA256: expected,
		Actor:                 apiActor(principal),
		Now:                   time.Now().UTC(),
	}); err != nil {
		writeMemoryMutationError(w, r, err, storeID, memoryID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": memoryID, "type": "memory_deleted"})
}

func (h *Handler) listVersionsRoute(w http.ResponseWriter, r *http.Request) {
	h.listVersions(w, r, chi.URLParam(r, "memory_store_id"))
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request, storeID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	view, err := parseView(r, "basic")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	limit, err := parseLimit(r, 100)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if view == "full" && limit > 20 {
		limit = 20
	}
	cursor, err := decodeVersionCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtGTE, err := parseOptionalTime(r, "created_at[gte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLTE, err := parseOptionalTime(r, "created_at[lte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	operation := strings.TrimSpace(r.URL.Query().Get("operation"))
	if operation != "" && operation != "created" && operation != "modified" && operation != "deleted" {
		writeBadRequest(w, r, errors.New("operation must be created, modified, or deleted"))
		return
	}
	store, err := h.memoryStoreForRead(r.Context(), principal, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	records, hasMore, err := h.db.ListMemoryVersionsPage(r.Context(), db.ListMemoryVersionsPageParams{
		WorkspaceID:           store.WorkspaceID,
		MemoryStoreExternalID: storeID,
		Limit:                 limit,
		Cursor:                cursor,
		MemoryExternalID:      strings.TrimSpace(r.URL.Query().Get("memory_id")),
		Operation:             operation,
		APIKeyExternalID:      strings.TrimSpace(r.URL.Query().Get("api_key_id")),
		SessionID:             strings.TrimSpace(r.URL.Query().Get("session_id")),
		CreatedAtGTE:          createdAtGTE,
		CreatedAtLTE:          createdAtLTE,
	})
	if err != nil {
		writeVersionLoadError(w, r, err, storeID, "")
		return
	}
	data := make([]versionResponse, 0, len(records))
	for _, record := range records {
		resp, err := h.responseFromVersion(r.Context(), record, view)
		if err != nil {
			log.Printf("read memory version list content version_id=%s: %v", record.ExternalID, err)
			writeAPIError(w, r, "Could not read memory version content")
			return
		}
		data = append(data, resp)
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeVersionCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, versionPageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieveVersion(w, r, chi.URLParam(r, "memory_store_id"), chi.URLParam(r, "memory_version_id"))
}

func (h *Handler) retrieveVersion(w http.ResponseWriter, r *http.Request, storeID, versionID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	view, err := parseView(r, "full")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	store, err := h.memoryStoreForRead(r.Context(), principal, storeID)
	if err != nil {
		writeStoreLoadError(w, r, err, storeID)
		return
	}
	record, err := h.db.GetMemoryVersion(r.Context(), store.WorkspaceID, storeID, versionID)
	if err != nil {
		writeVersionLoadError(w, r, err, storeID, versionID)
		return
	}
	resp, err := h.responseFromVersion(r.Context(), record, view)
	if err != nil {
		log.Printf("read memory version content version_id=%s: %v", versionID, err)
		writeAPIError(w, r, "Could not read memory version content")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) redactVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.redactVersion(w, r, chi.URLParam(r, "memory_store_id"), chi.URLParam(r, "memory_version_id"))
}

func (h *Handler) redactVersion(w http.ResponseWriter, r *http.Request, storeID, versionID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, ref, err := h.db.RedactMemoryVersion(r.Context(), principal.WorkspaceID, storeID, versionID, apiActor(principal), time.Now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			writeBadRequest(w, r, errors.New("cannot redact the active memory version"))
			return
		}
		writeVersionLoadError(w, r, err, storeID, versionID)
		return
	}
	if ref != nil {
		h.deleteObjectOrEnqueue(r.Context(), *ref)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVersionRecord(record, nil))
}

func (h *Handler) responseFromMemory(ctx context.Context, record db.Memory, view string) (memoryResponse, error) {
	if view != "full" {
		return responseFromMemoryRecord(record, nil), nil
	}
	content, err := h.readObjectContent(ctx, record.S3Key, record.ContentSizeBytes)
	if err != nil {
		return memoryResponse{}, err
	}
	return responseFromMemoryRecord(record, &content), nil
}

func (h *Handler) responseFromMemoryWithKnownContent(record db.Memory, view, content string) memoryResponse {
	if view == "full" {
		return responseFromMemoryRecord(record, &content)
	}
	return responseFromMemoryRecord(record, nil)
}

func (h *Handler) responseFromVersion(ctx context.Context, record db.MemoryVersion, view string) (versionResponse, error) {
	var content *string
	if view == "full" && record.Operation != "deleted" && record.RedactedAt == nil && record.S3Key != nil {
		size := int64(0)
		if record.ContentSizeBytes != nil {
			size = *record.ContentSizeBytes
		}
		value, err := h.readObjectContent(ctx, *record.S3Key, size)
		if err != nil {
			return versionResponse{}, err
		}
		content = &value
	}
	return responseFromVersionRecord(record, content), nil
}

func (h *Handler) readObjectContent(ctx context.Context, key string, expectedSize int64) (string, error) {
	object, err := h.store.Get(ctx, key)
	if err != nil {
		return "", err
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, maxMemoryContentBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) != expectedSize {
		log.Printf("memory object size mismatch key=%s bytes=%d expected=%d", key, len(data), expectedSize)
	}
	return string(data), nil
}

func (h *Handler) newMemoryObjectIDs(workspaceUUID, storeUUID string) (memoryID, versionID, memoryUUID, versionUUID, objectKey string, err error) {
	memoryID, err = ids.New("mem_")
	if err != nil {
		return "", "", "", "", "", err
	}
	versionID, err = ids.New("memver_")
	if err != nil {
		return "", "", "", "", "", err
	}
	memoryUUID = uuid.NewString()
	versionUUID = uuid.NewString()
	objectKey = memoryObjectKey(workspaceUUID, storeUUID, memoryUUID, versionUUID)
	return memoryID, versionID, memoryUUID, versionUUID, objectKey, nil
}

func memoryObjectKey(workspaceUUID, storeUUID, memoryUUID, versionUUID string) string {
	return fmt.Sprintf("workspaces/%s/memory_stores/%s/memories/%s/versions/%s/content", workspaceUUID, storeUUID, memoryUUID, versionUUID)
}

func (h *Handler) cleanupUploadedObjectAfterMetadataFailure(ctx context.Context, ref db.ObjectRef) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	h.deleteObjectOrEnqueue(cleanupCtx, ref)
}

func (h *Handler) deleteObjectOrEnqueue(ctx context.Context, ref db.ObjectRef) {
	if ref.Key == "" {
		return
	}
	if err := h.store.Delete(ctx, ref.Key); err != nil {
		log.Printf("delete memory object resource_type=%s resource_id=%s key=%s: %v", ref.ResourceType, ref.ResourceID, ref.Key, err)
		if enqueueErr := h.db.EnqueueObjectCleanupResourceJob(ctx, ref.WorkspaceID, ref.Bucket, ref.Key, ref.ResourceType, ref.ResourceID); enqueueErr != nil {
			log.Printf("enqueue memory object cleanup resource_type=%s resource_id=%s key=%s: %v", ref.ResourceType, ref.ResourceID, ref.Key, enqueueErr)
		}
	}
}

func responseFromStore(record db.MemoryStore) storeResponse {
	metadata := record.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return storeResponse{
		ID:          record.ExternalID,
		CreatedAt:   formatTime(record.CreatedAt),
		Name:        record.Name,
		Type:        "memory_store",
		UpdatedAt:   formatTime(record.UpdatedAt),
		ArchivedAt:  optionalTime(record.ArchivedAt),
		Description: record.Description,
		Metadata:    metadata,
	}
}

func responseFromMemoryRecord(record db.Memory, content *string) memoryResponse {
	return memoryResponse{
		ID:               record.ExternalID,
		ContentSHA256:    record.ContentSHA256,
		ContentSizeBytes: record.ContentSizeBytes,
		CreatedAt:        formatTime(record.CreatedAt),
		MemoryStoreID:    record.MemoryStoreExternalID,
		MemoryVersionID:  record.CurrentVersionExternalID,
		Path:             record.Path,
		Type:             "memory",
		UpdatedAt:        formatTime(record.UpdatedAt),
		Content:          content,
	}
}

func responseFromVersionRecord(record db.MemoryVersion, content *string) versionResponse {
	return versionResponse{
		ID:               record.ExternalID,
		CreatedAt:        formatTime(record.CreatedAt),
		MemoryID:         record.MemoryExternalID,
		MemoryStoreID:    record.MemoryStoreExternalID,
		Operation:        record.Operation,
		Type:             "memory_version",
		Content:          content,
		ContentSHA256:    record.ContentSHA256,
		ContentSizeBytes: record.ContentSizeBytes,
		CreatedBy:        responseFromActor(record.CreatedBy),
		Path:             record.Path,
		RedactedAt:       optionalTime(record.RedactedAt),
		RedactedBy:       optionalActor(record.RedactedBy),
	}
}

func responseFromActor(actor db.MemoryActor) actorResponse {
	return actorResponse{
		Type:      actor.Type,
		APIKeyID:  actor.APIKeyExternalID,
		SessionID: actor.SessionID,
		UserID:    actor.UserID,
	}
}

func optionalActor(actor *db.MemoryActor) *actorResponse {
	if actor == nil {
		return nil
	}
	value := responseFromActor(*actor)
	return &value
}

func apiActor(principal auth.Principal) db.MemoryActor {
	return db.MemoryActor{
		Type:             "api_actor",
		APIKeyID:         principal.APIKeyID,
		APIKeyExternalID: principal.APIKeyExternalID,
	}
}

func rollupMemoryItems(records []db.Memory, pathPrefix string, depth int) []memoryListItem {
	seenPrefixes := map[string]struct{}{}
	items := make([]memoryListItem, 0, len(records))
	for i := range records {
		record := records[i]
		if prefix, ok := rollupPrefix(record.Path, pathPrefix, depth); ok {
			if _, seen := seenPrefixes[prefix]; seen {
				continue
			}
			seenPrefixes[prefix] = struct{}{}
			value := prefix
			items = append(items, memoryListItem{Path: prefix, Prefix: &value})
			continue
		}
		items = append(items, memoryListItem{Path: record.Path, Memory: &record})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

func rollupPrefix(path, pathPrefix string, depth int) (string, bool) {
	relative := strings.TrimPrefix(path, pathPrefix)
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" {
		return "", false
	}
	segments := strings.Split(relative, "/")
	if len(segments) <= depth {
		return "", false
	}
	prefixSegments := segments[:depth]
	base := pathPrefix
	if base == "" {
		base = "/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + strings.Join(prefixSegments, "/") + "/", true
}

func decodeObjectBody(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMemoryBodySize)
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil {
		return nil, errors.New("Invalid JSON body")
	}
	if fields == nil {
		return nil, errors.New("JSON body must be an object")
	}
	return fields, nil
}

func parseRequiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return parseRequiredRawString(raw, name)
}

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", fmt.Errorf("%s is required", name)
	}
	value, err := parseRawString(raw, name)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

func optionalStringWithDefault(raw json.RawMessage, fallback, name string) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return fallback, nil
	}
	return parseRawString(raw, name)
}

func parseRawString(raw json.RawMessage, name string) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value, nil
}

func parseRequiredContent(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	if isJSONNull(raw) {
		return "", fmt.Errorf("%s cannot be null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if len([]byte(value)) > maxMemoryContentBytes {
		return "", fmt.Errorf("%s must be at most 102400 bytes", name)
	}
	return value, nil
}

func parseOptionalContent(fields map[string]json.RawMessage, name string) (string, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return "", false, nil
	}
	if isJSONNull(raw) {
		return "", false, fmt.Errorf("%s cannot be null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, fmt.Errorf("%s must be a string", name)
	}
	if len([]byte(value)) > maxMemoryContentBytes {
		return "", false, fmt.Errorf("%s must be at most 102400 bytes", name)
	}
	return value, true, nil
}

func parseOptionalMemoryPath(fields map[string]json.RawMessage, name string) (string, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return "", false, nil
	}
	path, err := parseRequiredRawString(raw, name)
	if err != nil {
		return "", false, err
	}
	if err := validateMemoryPath(path); err != nil {
		return "", false, err
	}
	return path, true, nil
}

func parsePrecondition(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("precondition must be an object")
	}
	typ, err := parseRequiredStringField(payload, "type")
	if err != nil {
		return nil, err
	}
	if typ != "content_sha256" {
		return nil, errors.New("precondition.type must be content_sha256")
	}
	value, err := parseRequiredStringField(payload, "content_sha256")
	if err != nil {
		return nil, err
	}
	if !isLowerHex64(value) {
		return nil, errors.New("precondition.content_sha256 must be a 64-character lowercase hex string")
	}
	return &value, nil
}

func normalizeMetadata(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, errors.New("metadata must be an object with string values")
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return marshalRaw(metadata)
}

func patchMetadata(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var metadata map[string]string
	if len(current) > 0 && !isJSONNull(current) {
		if err := json.Unmarshal(current, &metadata); err != nil {
			return nil, errors.New("stored metadata is invalid")
		}
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	var patch map[string]*string
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	for key, value := range patch {
		if value == nil {
			delete(metadata, key)
			continue
		}
		metadata[key] = *value
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return marshalRaw(metadata)
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > 16 {
		return errors.New("metadata must contain at most 16 keys")
	}
	for key, value := range metadata {
		if key == "" || len([]byte(key)) > 64 {
			return errors.New("metadata keys must be between 1 and 64 bytes")
		}
		if len([]byte(value)) > 512 {
			return errors.New("metadata values must be at most 512 bytes")
		}
	}
	return nil
}

func validateStoreName(value string) error {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 255 {
		return errors.New("name must be between 1 and 255 characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("name must not contain control characters")
		}
	}
	return nil
}

func validateDescription(value string) error {
	if utf8.RuneCountInString(value) > 1024 {
		return errors.New("description must be at most 1024 characters")
	}
	return nil
}

func validateMemoryPath(path string) error {
	if path == "" || len([]byte(path)) > maxMemoryPathBytes {
		return errors.New("path must be between 1 and 1024 bytes")
	}
	if !utf8.ValidString(path) {
		return errors.New("path must be valid UTF-8")
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New("path must start with /")
	}
	if path == "/" {
		return errors.New("path must contain at least one segment")
	}
	if strings.Contains(path, "//") || strings.HasSuffix(path, "/") {
		return errors.New("path must not contain empty segments")
	}
	if !norm.NFC.IsNormalString(path) {
		return errors.New("path must be NFC-normalized")
	}
	for _, r := range path {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("path must not contain control or format characters")
		}
	}
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "." || segment == ".." {
			return errors.New("path must not contain . or .. segments")
		}
	}
	return nil
}

func validatePathPrefix(pathPrefix string) error {
	if pathPrefix == "" {
		return nil
	}
	if len([]byte(pathPrefix)) > maxMemoryPathBytes {
		return errors.New("path_prefix must be at most 1024 bytes")
	}
	if !utf8.ValidString(pathPrefix) {
		return errors.New("path_prefix must be valid UTF-8")
	}
	if !strings.HasPrefix(pathPrefix, "/") {
		return errors.New("path_prefix must start with /")
	}
	if !norm.NFC.IsNormalString(pathPrefix) {
		return errors.New("path_prefix must be NFC-normalized")
	}
	if strings.Contains(pathPrefix, "//") {
		return errors.New("path_prefix must not contain empty segments")
	}
	for _, r := range pathPrefix {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("path_prefix must not contain control or format characters")
		}
	}
	trimmed := strings.Trim(pathPrefix, "/")
	if trimmed == "" {
		return nil
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "." || segment == ".." {
			return errors.New("path_prefix must not contain . or .. segments")
		}
	}
	return nil
}

func parseView(r *http.Request, fallback string) (string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("view"))
	if raw == "" {
		return fallback, nil
	}
	if raw != "basic" && raw != "full" {
		return "", errors.New("view must be basic or full")
	}
	return raw, nil
}

func parseLimit(r *http.Request, max int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > max {
		return 0, fmt.Errorf("limit must be between 1 and %d", max)
	}
	return limit, nil
}

func parseOrder(r *http.Request, fallback string) (string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("order"))
	if raw == "" {
		return fallback, nil
	}
	if raw != "asc" && raw != "desc" {
		return "", errors.New("order must be asc or desc")
	}
	return raw, nil
}

func parseMemoryOrderBy(r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("order_by"))
	if raw == "" {
		return "path", nil
	}
	switch raw {
	case "path", "created_at", "updated_at":
		return raw, nil
	default:
		return "", errors.New("order_by must be path, created_at, or updated_at")
	}
}

func parseOptionalDepth(r *http.Request) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("depth"))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return nil, errors.New("depth must be at least 1")
	}
	return &value, nil
}

func parseOptionalTime(r *http.Request, name string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339", name)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func parseOptionalBool(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return value, nil
}

func encodeStoreCursor(store db.MemoryStore) string {
	return encodeCursor(map[string]any{"created_at": store.CreatedAt.UTC().Format(time.RFC3339Nano), "id": store.ID})
}

func decodeStoreCursor(raw string) (*db.MemoryStorePageCursor, error) {
	payload, err := decodeCursorPayload(raw)
	if err != nil || payload == nil {
		return nil, err
	}
	id, ok := payload["id"].(float64)
	createdRaw, _ := payload["created_at"].(string)
	if !ok || id <= 0 || createdRaw == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.MemoryStorePageCursor{CreatedAt: createdAt.UTC(), ID: int64(id)}, nil
}

func encodeMemoryCursor(memory db.Memory) string {
	return encodeCursor(map[string]any{
		"path":       memory.Path,
		"created_at": memory.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": memory.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"id":         memory.ID,
	})
}

func decodeMemoryCursor(raw string) (*db.MemoryPageCursor, error) {
	payload, err := decodeCursorPayload(raw)
	if err != nil || payload == nil {
		return nil, err
	}
	path, _ := payload["path"].(string)
	id, ok := payload["id"].(float64)
	if !ok || id <= 0 {
		return nil, errors.New("page is invalid")
	}
	cursor := &db.MemoryPageCursor{Path: path, ID: int64(id)}
	if createdRaw, _ := payload["created_at"].(string); createdRaw != "" {
		createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
		if err != nil {
			return nil, errors.New("page is invalid")
		}
		cursor.CreatedAt = createdAt.UTC()
	}
	if updatedRaw, _ := payload["updated_at"].(string); updatedRaw != "" {
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedRaw)
		if err != nil {
			return nil, errors.New("page is invalid")
		}
		cursor.UpdatedAt = updatedAt.UTC()
	}
	return cursor, nil
}

func encodeVersionCursor(version db.MemoryVersion) string {
	return encodeCursor(map[string]any{"created_at": version.CreatedAt.UTC().Format(time.RFC3339Nano), "id": version.ID})
}

func decodeVersionCursor(raw string) (*db.MemoryVersionPageCursor, error) {
	payload, err := decodeCursorPayload(raw)
	if err != nil || payload == nil {
		return nil, err
	}
	id, ok := payload["id"].(float64)
	createdRaw, _ := payload["created_at"].(string)
	if !ok || id <= 0 || createdRaw == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.MemoryVersionPageCursor{CreatedAt: createdAt.UTC(), ID: int64(id)}, nil
}

func encodeDepthCursor(path string) string {
	return encodeCursor(map[string]any{"path": path})
}

func decodeDepthCursor(raw string) (string, error) {
	payload, err := decodeCursorPayload(raw)
	if err != nil || payload == nil {
		return "", err
	}
	path, _ := payload["path"].(string)
	if path == "" {
		return "", errors.New("page is invalid")
	}
	return path, nil
}

func encodeCursor(value any) string {
	data, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursorPayload(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.New("page is invalid")
	}
	return payload, nil
}

func requireAPIKey(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	if principal.CredentialType != "" &&
		principal.CredentialType != auth.CredentialTypeAPIKey &&
		principal.CredentialType != auth.CredentialTypePlatformSession {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "API key authentication required"))
		return auth.Principal{}, false
	}
	return principal, true
}

func fieldOrDefault(fields map[string]json.RawMessage, name, fallback string) json.RawMessage {
	if raw, ok := fields[name]; ok {
		return raw
	}
	return json.RawMessage(fallback)
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func isLowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ptrString(value string) *string {
	return &value
}

func ptrInt64(value int64) *int64 {
	return &value
}

func optionalPath(path string, ok bool) *string {
	if !ok {
		return nil
	}
	return &path
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	value := formatTime(*t)
	return &value
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
}

func writeAPIError(w http.ResponseWriter, r *http.Request, message string) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", message))
}

func writeStoreLoadError(w http.ResponseWriter, r *http.Request, err error, storeID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Memory store not found: "+storeID))
		return
	}
	log.Printf("memory store operation: %v", err)
	writeAPIError(w, r, "Memory store operation failed")
}

func writeMemoryLoadError(w http.ResponseWriter, r *http.Request, err error, storeID, memoryID string) {
	if errors.Is(err, db.ErrNotFound) {
		message := "Memory store not found: " + storeID
		if memoryID != "" {
			message = "Memory not found: " + memoryID
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", message))
		return
	}
	log.Printf("memory operation: %v", err)
	writeAPIError(w, r, "Memory operation failed")
}

func writeVersionLoadError(w http.ResponseWriter, r *http.Request, err error, storeID, versionID string) {
	if errors.Is(err, db.ErrNotFound) {
		message := "Memory store not found: " + storeID
		if versionID != "" {
			message = "Memory version not found: " + versionID
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", message))
		return
	}
	log.Printf("memory version operation: %v", err)
	writeAPIError(w, r, "Memory version operation failed")
}

func writeMemoryMutationError(w http.ResponseWriter, r *http.Request, err error, storeID, memoryID string) {
	var pathConflict *db.MemoryPathConflictError
	if errors.As(err, &pathConflict) {
		writePathConflict(w, r, pathConflict)
		return
	}
	if errors.Is(err, db.ErrPreconditionFailed) {
		writeMemorySpecificError(w, r, http.StatusConflict, "memory_precondition_failed_error", "Memory precondition failed", nil)
		return
	}
	if errors.Is(err, db.ErrInvalidState) {
		writeBadRequest(w, r, errors.New("memory store must not be archived"))
		return
	}
	if errors.Is(err, db.ErrNotFound) {
		if memoryID == "" {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Memory store not found: "+storeID))
		} else {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Memory not found: "+memoryID))
		}
		return
	}
	if errors.Is(err, db.ErrDuplicate) {
		writeMemorySpecificError(w, r, http.StatusConflict, "memory_path_conflict_error", "Memory path conflicts with existing memory", nil)
		return
	}
	log.Printf("memory mutation: %v", err)
	writeAPIError(w, r, "Memory mutation failed")
}

func writePathConflict(w http.ResponseWriter, r *http.Request, err *db.MemoryPathConflictError) {
	extra := map[string]any{
		"conflicting_memory_id": err.ConflictingMemoryID,
		"conflicting_path":      err.ConflictingPath,
		"conflict_error": map[string]string{
			"type":    "memory_path_conflict_error",
			"message": "Memory path conflicts with existing memory",
		},
	}
	writeMemorySpecificError(w, r, http.StatusConflict, "memory_path_conflict_error", "Memory path conflicts with existing memory", extra)
}

func writeMemorySpecificError(w http.ResponseWriter, r *http.Request, status int, typ, message string, extra map[string]any) {
	errorBody := map[string]any{"type": typ, "message": message}
	for key, value := range extra {
		errorBody[key] = value
	}
	httpapi.WriteJSON(w, status, map[string]any{
		"type":       "error",
		"request_id": httpapi.RequestID(r.Context()),
		"error":      errorBody,
	})
}
