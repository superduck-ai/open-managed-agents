package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/memorypath"
)

func (h *Handler) WithRuntimeResolver(resolver *agentruntime.Resolver) *Handler {
	h.runtimeResolver = resolver
	return h
}

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func (h *Handler) writeRuntimeError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	adapter := h.errorAdapter
	if adapter == nil {
		adapter = httpapi.NewErrorAdapter(h.logger)
	}
	adapter.Write(w, r, err)
}

func (h *Handler) resolveExternalMemoryRuntime(ctx context.Context, principal auth.Principal) (agentruntime.Runtime, agentruntime.MemoryRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, memoryRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	memRT, ok := rt.(agentruntime.MemoryRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, memoryRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, memRT, scope, nil
}

func (h *Handler) createStoreExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	body, err := httpapi.DecodeObjectBodyAs[memoryStoreMutationRequest](w, r, maxMemoryBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	name, err := parseRequiredRawString(body.Name, "name")
	if err != nil {
		return invalidRequest(err)
	}
	if err := validateStoreName(name); err != nil {
		return invalidRequest(err)
	}
	description, err := optionalStringWithDefault(body.Description, "", "description")
	if err != nil {
		return invalidRequest(err)
	}
	if err := validateDescription(description); err != nil {
		return invalidRequest(err)
	}
	metadata, err := decodeMemoryMetadataMap(body.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := memRT.CreateMemoryStore(r.Context(), scope, agentruntime.CreateMemoryStoreInput{
		Name:        name,
		Description: description,
		Metadata:    metadata,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeStore(created))
	return nil
}

func (h *Handler) listStoresExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := parseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseExternalOffsetCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGTE, err := parseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := parseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := memRT.ListMemoryStores(r.Context(), scope, agentruntime.ListMemoryStoresQuery{
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		return err
	}
	data := make([]storeResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromRuntimeStore(item))
	}
	var nextPage *string
	if page.HasMore {
		cursor := page.NextCursor
		if cursor == "" {
			step := len(data)
			if step == 0 {
				step = limit
			}
			cursor = strconv.Itoa(offset + step)
		}
		nextPage = &cursor
	}
	httpapi.WriteJSON(w, http.StatusOK, storePageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveStoreExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	store, err := memRT.GetMemoryStore(r.Context(), scope, storeID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeStore(store))
	return nil
}

func (h *Handler) updateStoreExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	body, err := httpapi.DecodeObjectBodyAs[memoryStoreMutationRequest](w, r, maxMemoryBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	input, hasMetadataPatch, err := buildExternalUpdateStoreInput(body)
	if err != nil {
		return invalidRequest(err)
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if hasMetadataPatch {
		current, err := memRT.GetMemoryStore(r.Context(), scope, storeID)
		if err != nil {
			return err
		}
		merged, err := mergeStoreMetadataPatch(current.Metadata, body.Metadata)
		if err != nil {
			return invalidRequest(err)
		}
		input.Metadata = merged
	}
	updated, err := memRT.UpdateMemoryStore(r.Context(), scope, storeID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeStore(updated))
	return nil
}

func (h *Handler) archiveStoreExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	archived, err := memRT.ArchiveMemoryStore(r.Context(), scope, storeID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeStore(archived))
	return nil
}

func (h *Handler) deleteStoreExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := memRT.DeleteMemoryStore(r.Context(), scope, storeID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": storeID, "type": "memory_store_deleted"})
	return nil
}

func (h *Handler) createMemoryExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	view, err := parseView(r, "basic")
	if err != nil {
		return invalidRequest(err)
	}
	body, err := httpapi.DecodeObjectBodyAs[memoryMutationRequest](w, r, maxMemoryBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	content, err := parseRequiredContent(body.Content, "content")
	if err != nil {
		return invalidRequest(err)
	}
	path, err := parseRequiredRawString(body.Path, "path")
	if err != nil {
		return invalidRequest(err)
	}
	if err := memorypath.Validate(path); err != nil {
		return invalidRequest(err)
	}
	expectedHash, err := parsePrecondition(body.Precondition)
	if err != nil {
		return invalidRequest(err)
	}
	var precondition *agentruntime.MemoryPrecondition
	if expectedHash != nil {
		precondition = &agentruntime.MemoryPrecondition{
			Type:          "content_sha256",
			ContentSHA256: *expectedHash,
		}
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := memRT.CreateMemory(r.Context(), scope, storeID, agentruntime.CreateMemoryInput{
		Path:         path,
		Content:      content,
		View:         view,
		Precondition: precondition,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeMemory(created, view))
	return nil
}

func (h *Handler) listMemoriesExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	view, err := parseView(r, "basic")
	if err != nil {
		return invalidRequest(err)
	}
	limit, err := parseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	if view == "full" && limit > 20 {
		limit = 20
	}
	order, err := parseOrder(r, "asc")
	if err != nil {
		return invalidRequest(err)
	}
	orderBy, err := parseMemoryOrderBy(r)
	if err != nil {
		return invalidRequest(err)
	}
	pathPrefix, err := parseMemoryListPrefix(r)
	if err != nil {
		return invalidRequest(err)
	}
	depth, err := parseOptionalDepth(r)
	if err != nil {
		return invalidRequest(err)
	}
	pageToken := strings.TrimSpace(r.URL.Query().Get("page"))
	if depth != nil {
		if orderBy != "path" {
			return invalidRequest(errors.New("order_by must be path when depth is set"))
		}
		cursor, decodeErr := decodeDepthCursor(pageToken)
		if decodeErr != nil {
			return invalidRequest(decodeErr)
		}
		pageToken = cursor
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	result, err := memRT.ListMemories(r.Context(), scope, storeID, agentruntime.ListMemoriesQuery{
		PathPrefix: pathPrefix,
		Depth:      depth,
		View:       view,
		Order:      order,
		OrderBy:    orderBy,
		Limit:      limit,
		Page:       pageToken,
	})
	if err != nil {
		return err
	}
	if depth != nil {
		httpapi.WriteJSON(w, http.StatusOK, depthPageResponseFromRuntime(result, view, order))
		return nil
	}
	data := make([]any, 0, len(result.Items))
	for _, item := range result.Items {
		data = append(data, responseFromRuntimeMemory(item, view))
	}
	var nextPage *string
	if result.HasMore && result.NextPage != "" {
		value := result.NextPage
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, memoryPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveMemoryExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID, memoryID string) error {
	view, err := parseView(r, "full")
	if err != nil {
		return invalidRequest(err)
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	mem, err := memRT.GetMemory(r.Context(), scope, storeID, memoryID, agentruntime.GetMemoryQuery{View: view})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeMemory(mem, view))
	return nil
}

func (h *Handler) updateMemoryExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID, memoryID string) error {
	view, err := parseView(r, "basic")
	if err != nil {
		return invalidRequest(err)
	}
	body, err := httpapi.DecodeObjectBodyAs[memoryMutationRequest](w, r, maxMemoryBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	content, contentPresent, err := parseOptionalContent(body.Content, "content")
	if err != nil {
		return invalidRequest(err)
	}
	path, pathPresent, err := parseOptionalMemoryPath(body.Path, "path")
	if err != nil {
		return invalidRequest(err)
	}
	expectedHash, err := parsePrecondition(body.Precondition)
	if err != nil {
		return invalidRequest(err)
	}
	input := agentruntime.UpdateMemoryInput{
		Path: optionalPath(path, pathPresent),
		View: view,
	}
	if contentPresent {
		input.Content = &content
	}
	if expectedHash != nil {
		input.Precondition = &agentruntime.MemoryPrecondition{
			Type:          "content_sha256",
			ContentSHA256: *expectedHash,
		}
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	updated, err := memRT.UpdateMemory(r.Context(), scope, storeID, memoryID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeMemory(updated, view))
	return nil
}

func (h *Handler) deleteMemoryExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID, memoryID string) error {
	expectedHash := strings.TrimSpace(r.URL.Query().Get("expected_content_sha256"))
	var expected *string
	if expectedHash != "" {
		if !isLowerHex64(expectedHash) {
			return invalidRequest(errors.New("expected_content_sha256 must be a 64-character lowercase hex string"))
		}
		expected = &expectedHash
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := memRT.DeleteMemory(r.Context(), scope, storeID, memoryID, agentruntime.DeleteMemoryInput{
		ExpectedContentSHA256: expected,
	}); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": memoryID, "type": "memory_deleted"})
	return nil
}

func (h *Handler) listVersionsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID string) error {
	view, err := parseView(r, "basic")
	if err != nil {
		return invalidRequest(err)
	}
	limit, err := parseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	if view == "full" && limit > 20 {
		limit = 20
	}
	createdAtGTE, err := parseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := parseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	operation := strings.TrimSpace(r.URL.Query().Get("operation"))
	if operation != "" && operation != "created" && operation != "modified" && operation != "deleted" {
		return invalidRequest(errors.New("operation must be created, modified, or deleted"))
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := memRT.ListMemoryVersions(r.Context(), scope, storeID, agentruntime.ListMemoryVersionsQuery{
		MemoryID:     strings.TrimSpace(r.URL.Query().Get("memory_id")),
		Path:         strings.TrimSpace(r.URL.Query().Get("path")),
		Operation:    operation,
		APIKeyID:     strings.TrimSpace(r.URL.Query().Get("api_key_id")),
		SessionID:    strings.TrimSpace(r.URL.Query().Get("session_id")),
		View:         view,
		Limit:        limit,
		Page:         strings.TrimSpace(r.URL.Query().Get("page")),
		CreatedAtGTE: createdAtGTE,
		CreatedAtLTE: createdAtLTE,
	})
	if err != nil {
		return err
	}
	data := make([]versionResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromRuntimeVersion(item, view))
	}
	var nextPage *string
	if page.HasMore && page.NextCursor != "" {
		value := page.NextCursor
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, versionPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveVersionExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID, versionID string) error {
	view, err := parseView(r, "full")
	if err != nil {
		return invalidRequest(err)
	}
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	version, err := memRT.GetMemoryVersion(r.Context(), scope, storeID, versionID, agentruntime.GetMemoryVersionQuery{View: view})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeVersion(version, view))
	return nil
}

func (h *Handler) redactVersionExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, storeID, versionID string) error {
	rt, memRT, scope, err := h.resolveExternalMemoryRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	redacted, err := memRT.RedactMemoryVersion(r.Context(), scope, storeID, versionID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeVersion(redacted, "basic"))
	return nil
}

func buildExternalUpdateStoreInput(body *memoryStoreMutationRequest) (agentruntime.UpdateMemoryStoreInput, bool, error) {
	var input agentruntime.UpdateMemoryStoreInput
	if len(body.Name) > 0 {
		name, err := parseRequiredRawString(body.Name, "name")
		if err != nil {
			return agentruntime.UpdateMemoryStoreInput{}, false, err
		}
		if err := validateStoreName(name); err != nil {
			return agentruntime.UpdateMemoryStoreInput{}, false, err
		}
		input.Name = &name
	}
	if len(body.Description) > 0 {
		description := ""
		if !isJSONNull(body.Description) {
			parsed, err := parseRawString(body.Description, "description")
			if err != nil {
				return agentruntime.UpdateMemoryStoreInput{}, false, err
			}
			description = parsed
		}
		if err := validateDescription(description); err != nil {
			return agentruntime.UpdateMemoryStoreInput{}, false, err
		}
		input.Description = &description
	}
	if len(body.Metadata) > 0 {
		if isJSONNull(body.Metadata) {
			input.Metadata = map[string]string{}
			return input, false, nil
		}
		return input, true, nil
	}
	return input, false, nil
}

func decodeMemoryMetadataMap(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	normalized, err := normalizeMetadata(raw)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	if err := json.Unmarshal(normalized, &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func mergeStoreMetadataPatch(current map[string]string, rawPatch json.RawMessage) (map[string]string, error) {
	currentRaw := json.RawMessage(`{}`)
	if len(current) > 0 {
		encoded, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		currentRaw = encoded
	}
	mergedRaw, err := patchMetadata(currentRaw, rawPatch)
	if err != nil {
		return nil, err
	}
	merged := map[string]string{}
	if err := json.Unmarshal(mergedRaw, &merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func parseExternalOffsetCursor(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errors.New("page is invalid")
	}
	return offset, nil
}

func responseFromRuntimeStore(store agentruntime.MemoryStore) storeResponse {
	metadata := json.RawMessage(`{}`)
	if len(store.Metadata) > 0 {
		if encoded, err := json.Marshal(store.Metadata); err == nil {
			metadata = encoded
		}
	}
	return storeResponse{
		ID:          store.ID,
		CreatedAt:   store.CreatedAt.UTC().Format(time.RFC3339Nano),
		Name:        store.Name,
		Type:        "memory_store",
		UpdatedAt:   formatTime(store.UpdatedAt),
		ArchivedAt:  optionalTime(store.ArchivedAt),
		Description: store.Description,
		Metadata:    metadata,
	}
}

func responseFromRuntimeMemory(mem agentruntime.Memory, view string) memoryResponse {
	var content *string
	if view == "full" && mem.Content != nil {
		value := *mem.Content
		content = &value
	}
	return memoryResponse{
		ID:               mem.ID,
		ContentSHA256:    mem.ContentSHA256,
		ContentSizeBytes: mem.ContentSizeBytes,
		CreatedAt:        formatTime(mem.CreatedAt),
		MemoryStoreID:    mem.MemoryStoreID,
		MemoryVersionID:  mem.MemoryVersionID,
		Path:             mem.Path,
		Type:             "memory",
		UpdatedAt:        formatTime(mem.UpdatedAt),
		Content:          content,
	}
}

func responseFromRuntimeVersion(ver agentruntime.MemoryVersion, view string) versionResponse {
	var content *string
	if view == "full" && !ver.Redacted && ver.Operation != "deleted" && ver.Content != nil {
		value := *ver.Content
		content = &value
	}
	createdBy := actorResponse{
		Type:      ver.CreatedBy.Type,
		APIKeyID:  ver.CreatedBy.APIKeyID,
		SessionID: ver.CreatedBy.SessionID,
		UserID:    ver.CreatedBy.UserID,
	}
	if createdBy.Type == "" {
		createdBy.Type = "api_actor"
	}
	var redactedBy *actorResponse
	if ver.RedactedBy != nil {
		redactedBy = &actorResponse{
			Type:      ver.RedactedBy.Type,
			APIKeyID:  ver.RedactedBy.APIKeyID,
			SessionID: ver.RedactedBy.SessionID,
			UserID:    ver.RedactedBy.UserID,
		}
	}
	return versionResponse{
		ID:               ver.ID,
		CreatedAt:        ver.CreatedAt.UTC().Format(time.RFC3339Nano),
		MemoryID:         ver.MemoryID,
		MemoryStoreID:    ver.MemoryStoreID,
		Operation:        ver.Operation,
		Type:             "memory_version",
		Content:          content,
		ContentSHA256:    ver.ContentSHA256,
		ContentSizeBytes: ver.ContentSizeBytes,
		CreatedBy:        createdBy,
		Path:             ver.Path,
		RedactedAt:       optionalTime(ver.RedactedAt),
		RedactedBy:       redactedBy,
	}
}

type runtimeDepthEntry struct {
	Path   string
	Memory *agentruntime.Memory
	Prefix string
}

func depthPageResponseFromRuntime(result agentruntime.MemoryListResult, view, order string) memoryDepthPageResponse {
	entries := make([]runtimeDepthEntry, 0, len(result.Items)+len(result.Prefixes))
	for i := range result.Items {
		mem := result.Items[i]
		entries = append(entries, runtimeDepthEntry{Path: mem.Path, Memory: &mem})
	}
	for _, prefix := range result.Prefixes {
		entries = append(entries, runtimeDepthEntry{Path: prefix, Prefix: prefix})
	}
	sort.Slice(entries, func(i, j int) bool {
		if order == "desc" {
			return entries[i].Path > entries[j].Path
		}
		return entries[i].Path < entries[j].Path
	})
	data := make([]any, 0, len(entries))
	prefixes := make([]memoryPrefixResponse, 0, len(result.Prefixes))
	for _, entry := range entries {
		if entry.Prefix != "" {
			p := memoryPrefixResponse{Path: entry.Prefix, Type: "memory_prefix"}
			data = append(data, p)
			prefixes = append(prefixes, p)
			continue
		}
		data = append(data, responseFromRuntimeMemory(*entry.Memory, view))
	}
	var nextPage *string
	if result.HasMore && result.NextPage != "" {
		encoded := encodeDepthCursor(result.NextPage)
		nextPage = &encoded
	}
	return memoryDepthPageResponse{
		Data:     data,
		Prefixes: prefixes,
		NextPage: nextPage,
	}
}
