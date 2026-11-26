package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const (
	memoryHydrateConcurrency = 5
	maxDepthScanPages        = 50
	depthScanPageLimit       = 100
)

type qoderMemoryStore struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	EntryCount  int64             `json:"entry_count"`
	TotalSize   int64             `json:"total_size"`
	Metadata    map[string]string `json:"metadata"`
	ArchivedAt  *time.Time        `json:"archived_at"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type qoderCreateMemoryStoreRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type qoderUpdateMemoryStoreRequest struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type memoryStoreBindingSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
}

type qoderFlexString string

func (s *qoderFlexString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*s = ""
		return nil
	}
	if trimmed[0] == '"' {
		var str string
		if err := json.Unmarshal(trimmed, &str); err != nil {
			return err
		}
		*s = qoderFlexString(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(trimmed, &num); err != nil {
		return err
	}
	*s = qoderFlexString(num.String())
	return nil
}

type qoderMemoryPrecondition struct {
	Type          string `json:"type"`
	ContentSHA256 string `json:"content_sha256"`
}

type qoderCreateMemoryRequest struct {
	Path         string                   `json:"path"`
	Content      string                   `json:"content"`
	Precondition *qoderMemoryPrecondition `json:"precondition,omitempty"`
}

type qoderUpdateMemoryRequest struct {
	Path         *string                  `json:"path,omitempty"`
	Content      *string                  `json:"content,omitempty"`
	Precondition *qoderMemoryPrecondition `json:"precondition,omitempty"`
}

type qoderMemory struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	MemoryStoreID    string          `json:"memory_store_id"`
	MemoryVersionID  string          `json:"memory_version_id"`
	Version          qoderFlexString `json:"version"`
	Path             string          `json:"path"`
	Content          *string         `json:"content"`
	ContentSizeBytes int64           `json:"content_size_bytes"`
	ContentSHA256    string          `json:"content_sha256"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type qoderMemoryPrefix struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (p *qoderMemoryPrefix) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		p.Path = s
		p.Type = "memory_prefix"
		return nil
	}
	var obj struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	p.Path = obj.Path
	p.Type = obj.Type
	return nil
}

type qoderMemoriesPage struct {
	Data     []qoderMemory       `json:"data"`
	Prefixes []qoderMemoryPrefix `json:"prefixes"`
	HasMore  bool                `json:"has_more"`
	FirstID  string              `json:"first_id"`
	LastID   string              `json:"last_id"`
	NextPage *string             `json:"next_page"`
}

type qoderMemoryActor struct {
	Type      string `json:"type"`
	APIKeyID  string `json:"api_key_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

type qoderMemoryVersion struct {
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	MemoryID         string            `json:"memory_id"`
	MemoryStoreID    string            `json:"memory_store_id"`
	Operation        string            `json:"operation"`
	Path             *string           `json:"path"`
	Content          *string           `json:"content"`
	ContentSizeBytes *int64            `json:"content_size_bytes"`
	ContentSHA256    *string           `json:"content_sha256"`
	CreatedBy        *qoderMemoryActor `json:"created_by"`
	Redacted         bool              `json:"redacted"`
	RedactedAt       *time.Time        `json:"redacted_at"`
	RedactedBy       *qoderMemoryActor `json:"redacted_by"`
	CreatedAt        time.Time         `json:"created_at"`
}

type qoderMemoryVersionsPage struct {
	Data     []qoderMemoryVersion `json:"data"`
	HasMore  bool                 `json:"has_more"`
	FirstID  string               `json:"first_id"`
	LastID   string               `json:"last_id"`
	NextPage *string              `json:"next_page"`
}

type foldedMemoryEntry struct {
	Path   string
	Memory *agentruntime.Memory
	Prefix string
}

func (r *Runtime) CreateMemoryStore(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateMemoryStoreInput) (agentruntime.MemoryStore, error) {
	reqBody := qoderCreateMemoryStoreRequest{
		Name:        input.Name,
		Description: input.Description,
		Metadata:    injectWatermark(input.Metadata, scope),
	}
	var remote qoderMemoryStore
	if err := r.doJSON(ctx, http.MethodPost, "/memory_stores", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.MemoryStore{}, err
	}
	omaID, err := ids.New("memstore_")
	if err != nil {
		return agentruntime.MemoryStore{}, agentruntime.UnavailableError("failed to generate memory store identifier", err)
	}
	summary, _ := json.Marshal(memoryStoreBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
		Archived:    isQoderStoreArchived(remote),
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeMemoryStore,
		OMAExternalID: omaID,
		RemoteID:      remote.ID,
	}, summary); err != nil {
		return agentruntime.MemoryStore{}, err
	}
	return mapQoderMemoryStore(omaID, remote), nil
}

func (r *Runtime) GetMemoryStore(ctx context.Context, scope agentruntime.Scope, storeID string) (agentruntime.MemoryStore, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.MemoryStore{}, err
	}
	var remote qoderMemoryStore
	if err := r.doJSON(ctx, http.MethodGet, "/memory_stores/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.MemoryStore{}, err
	}
	return mapQoderMemoryStore(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ListMemoryStores(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListMemoryStoresQuery) (agentruntime.Page[agentruntime.MemoryStore], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, "", "", query.IncludeArchived, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.MemoryStore]{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]agentruntime.MemoryStore, 0, len(rows))
	for _, row := range rows {
		var remote qoderMemoryStore
		var store agentruntime.MemoryStore
		if err := r.doJSON(ctx, http.MethodGet, "/memory_stores/"+url.PathEscape(row.RemoteID), nil, nil, nil, &remote); err != nil {
			r.logger.WarnContext(ctx, "qoder memory store hydrate failed, falling back to binding summary", "oma_id", row.OMAExternalID, "error", err)
			store = memoryStoreFromBindingSummary(row)
		} else {
			store = mapQoderMemoryStore(row.OMAExternalID, remote)
		}
		if query.CreatedAtGTE != nil && store.CreatedAt.Before(*query.CreatedAtGTE) {
			continue
		}
		if query.CreatedAtLTE != nil && store.CreatedAt.After(*query.CreatedAtLTE) {
			continue
		}
		items = append(items, store)
	}
	page := agentruntime.Page[agentruntime.MemoryStore]{
		Items:   items,
		HasMore: hasMore,
	}
	if len(items) > 0 {
		page.FirstID = items[0].ID
		page.LastID = items[len(items)-1].ID
	}
	return page, nil
}

func (r *Runtime) UpdateMemoryStore(ctx context.Context, scope agentruntime.Scope, storeID string, input agentruntime.UpdateMemoryStoreInput) (agentruntime.MemoryStore, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.MemoryStore{}, err
	}
	reqBody := qoderUpdateMemoryStoreRequest{
		Name:        input.Name,
		Description: input.Description,
	}
	if input.Metadata != nil {
		reqBody.Metadata = injectWatermark(input.Metadata, scope)
	}
	var remote qoderMemoryStore
	if err := r.doJSON(ctx, http.MethodPost, "/memory_stores/"+url.PathEscape(binding.RemoteID), nil, nil, reqBody, &remote); err != nil {
		return agentruntime.MemoryStore{}, err
	}
	summary, _ := json.Marshal(memoryStoreBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
		Archived:    isQoderStoreArchived(remote),
	})
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeMemoryStore, binding.OMAExternalID, "", summary); err != nil {
		r.logger.WarnContext(ctx, "failed to refresh memory store binding summary", "oma_id", binding.OMAExternalID, "error", err)
	}
	return mapQoderMemoryStore(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ArchiveMemoryStore(ctx context.Context, scope agentruntime.Scope, storeID string) (agentruntime.MemoryStore, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.MemoryStore{}, err
	}
	var remote qoderMemoryStore
	if err := r.doJSON(ctx, http.MethodPost, "/memory_stores/"+url.PathEscape(binding.RemoteID)+"/archive", nil, nil, nil, &remote); err != nil {
		return agentruntime.MemoryStore{}, err
	}
	summary, _ := json.Marshal(memoryStoreBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
		Archived:    true,
	})
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeMemoryStore, binding.OMAExternalID, "archived", summary); err != nil {
		return agentruntime.MemoryStore{}, err
	}
	return mapQoderMemoryStore(binding.OMAExternalID, remote), nil
}

func (r *Runtime) DeleteMemoryStore(ctx context.Context, scope agentruntime.Scope, storeID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return err
	}
	if err := r.doJSON(ctx, http.MethodDelete, "/memory_stores/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil); err != nil {
		if !isUpstreamNotFound(err) {
			return err
		}
	}
	return r.bindings.SoftDelete(ctx, scope, agentruntime.ResourceTypeMemoryStore, binding.OMAExternalID)
}

func (r *Runtime) CreateMemory(ctx context.Context, scope agentruntime.Scope, storeID string, input agentruntime.CreateMemoryInput) (agentruntime.Memory, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.Memory{}, err
	}
	reqBody := qoderCreateMemoryRequest{
		Path:         toQoderMemoryPath(input.Path),
		Content:      input.Content,
		Precondition: toQoderPrecondition(input.Precondition),
	}
	var remote qoderMemory
	if err := r.doJSON(ctx, http.MethodPost, "/memory_stores/"+url.PathEscape(binding.RemoteID)+"/memories", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Memory{}, err
	}
	mapped := mapQoderMemory(binding.OMAExternalID, remote, input.View)
	if input.View == "full" && mapped.Content == nil {
		content := input.Content
		mapped.Content = &content
	}
	return mapped, nil
}

func (r *Runtime) GetMemory(ctx context.Context, scope agentruntime.Scope, storeID string, memoryID string, query agentruntime.GetMemoryQuery) (agentruntime.Memory, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.Memory{}, err
	}
	var remote qoderMemory
	path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memories/" + url.PathEscape(memoryID)
	if err := r.doJSON(ctx, http.MethodGet, path, nil, nil, nil, &remote); err != nil {
		return agentruntime.Memory{}, err
	}
	view := query.View
	if view == "" {
		view = "full"
	}
	return mapQoderMemory(binding.OMAExternalID, remote, view), nil
}

func (r *Runtime) ListMemories(ctx context.Context, scope agentruntime.Scope, storeID string, query agentruntime.ListMemoriesQuery) (agentruntime.MemoryListResult, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.MemoryListResult{}, err
	}
	if query.Depth != nil {
		return r.listMemoriesWithDepth(ctx, binding, query)
	}
	params := url.Values{}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	if query.Page != "" {
		params.Set("page", query.Page)
	}
	if qoderPrefix := toQoderMemoryPathPrefix(query.PathPrefix); qoderPrefix != "" {
		params.Set("path_prefix", qoderPrefix)
	}
	if query.Order != "" {
		params.Set("order", query.Order)
	}
	if query.OrderBy != "" {
		params.Set("order_by", query.OrderBy)
	}
	var remotePage qoderMemoriesPage
	if err := r.doJSON(ctx, http.MethodGet, "/memory_stores/"+url.PathEscape(binding.RemoteID)+"/memories", params, nil, nil, &remotePage); err != nil {
		return agentruntime.MemoryListResult{}, err
	}
	items := make([]agentruntime.Memory, 0, len(remotePage.Data))
	for _, entry := range remotePage.Data {
		items = append(items, mapQoderMemory(binding.OMAExternalID, entry, query.View))
	}
	if query.View == "full" {
		if err := r.hydrateMemoriesContent(ctx, binding, items); err != nil {
			return agentruntime.MemoryListResult{}, err
		}
	}
	nextPage := resolveNextPageCursor(remotePage.NextPage, remotePage.HasMore, remotePage.LastID)
	return agentruntime.MemoryListResult{
		Items:    items,
		NextPage: nextPage,
		HasMore:  remotePage.HasMore || nextPage != "",
	}, nil
}

func (r *Runtime) listMemoriesWithDepth(ctx context.Context, binding db.RuntimeResourceBinding, query agentruntime.ListMemoriesQuery) (agentruntime.MemoryListResult, error) {
	allMemories, upstreamPrefixes, err := r.scanMemoriesForDepth(ctx, binding, query.PathPrefix)
	if err != nil {
		return agentruntime.MemoryListResult{}, err
	}
	folded := foldMemoriesForDepth(allMemories, upstreamPrefixes, query.PathPrefix, *query.Depth)
	order := query.Order
	if order == "" {
		order = "asc"
	}
	if order == "desc" {
		sort.Slice(folded, func(i, j int) bool { return folded[i].Path > folded[j].Path })
	}
	if query.Page != "" {
		filtered := folded[:0]
		for _, entry := range folded {
			if (order == "asc" && entry.Path > query.Page) || (order == "desc" && entry.Path < query.Page) {
				filtered = append(filtered, entry)
			}
		}
		folded = filtered
	}
	limit := normalizeListLimit(query.Limit)
	hasMore := len(folded) > limit
	if hasMore {
		folded = folded[:limit]
	}
	items := make([]agentruntime.Memory, 0, len(folded))
	prefixes := make([]string, 0)
	for _, entry := range folded {
		if entry.Prefix != "" {
			prefixes = append(prefixes, entry.Prefix)
			continue
		}
		items = append(items, *entry.Memory)
	}
	if query.View == "full" {
		if err := r.hydrateMemoriesContent(ctx, binding, items); err != nil {
			return agentruntime.MemoryListResult{}, err
		}
	}
	nextPage := ""
	if hasMore && len(folded) > 0 {
		nextPage = folded[len(folded)-1].Path
	}
	return agentruntime.MemoryListResult{
		Items:    items,
		Prefixes: prefixes,
		NextPage: nextPage,
		HasMore:  hasMore,
	}, nil
}

func (r *Runtime) scanMemoriesForDepth(ctx context.Context, binding db.RuntimeResourceBinding, pathPrefix string) ([]agentruntime.Memory, []string, error) {
	qoderPrefix := toQoderMemoryPathPrefix(pathPrefix)
	cursor := ""
	var memories []agentruntime.Memory
	var prefixes []string
	for pageIdx := 0; pageIdx < maxDepthScanPages; pageIdx++ {
		params := url.Values{}
		params.Set("limit", strconv.Itoa(depthScanPageLimit))
		if qoderPrefix != "" {
			params.Set("path_prefix", qoderPrefix)
		}
		if cursor != "" {
			params.Set("page", cursor)
		}
		var remotePage qoderMemoriesPage
		if err := r.doJSON(ctx, http.MethodGet, "/memory_stores/"+url.PathEscape(binding.RemoteID)+"/memories", params, nil, nil, &remotePage); err != nil {
			return nil, nil, err
		}
		for _, entry := range remotePage.Data {
			if entry.Type == "memory_prefix" {
				if p := normalizePrefixPath(entry.Path); p != "" {
					prefixes = append(prefixes, p)
				}
				continue
			}
			memories = append(memories, mapQoderMemory(binding.OMAExternalID, entry, "basic"))
		}
		for _, p := range remotePage.Prefixes {
			if normalized := normalizePrefixPath(p.Path); normalized != "" {
				prefixes = append(prefixes, normalized)
			}
		}
		next := resolveNextPageCursor(remotePage.NextPage, remotePage.HasMore, remotePage.LastID)
		if !remotePage.HasMore || next == "" || next == cursor {
			break
		}
		cursor = next
	}
	return memories, prefixes, nil
}

func (r *Runtime) hydrateMemoriesContent(ctx context.Context, binding db.RuntimeResourceBinding, items []agentruntime.Memory) error {
	if len(items) == 0 {
		return nil
	}
	hydrateCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, memoryHydrateConcurrency)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for idx := range items {
		if items[idx].Content != nil {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if hydrateCtx.Err() != nil {
				return
			}
			var remote qoderMemory
			path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memories/" + url.PathEscape(items[i].ID)
			if err := r.doJSON(hydrateCtx, http.MethodGet, path, nil, nil, nil, &remote); err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			items[i] = mapQoderMemory(binding.OMAExternalID, remote, "full")
		}(idx)
	}
	wg.Wait()
	return firstErr
}

func (r *Runtime) UpdateMemory(ctx context.Context, scope agentruntime.Scope, storeID string, memoryID string, input agentruntime.UpdateMemoryInput) (agentruntime.Memory, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.Memory{}, err
	}
	reqBody := qoderUpdateMemoryRequest{
		Content:      input.Content,
		Precondition: toQoderPrecondition(input.Precondition),
	}
	if input.Path != nil {
		qoderPath := toQoderMemoryPath(*input.Path)
		reqBody.Path = &qoderPath
	}
	var remote qoderMemory
	path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memories/" + url.PathEscape(memoryID)
	if err := r.doJSON(ctx, http.MethodPost, path, nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Memory{}, err
	}
	mapped := mapQoderMemory(binding.OMAExternalID, remote, input.View)
	if input.View == "full" && mapped.Content == nil {
		if input.Content != nil {
			content := *input.Content
			mapped.Content = &content
		} else {
			var fullRemote qoderMemory
			if err := r.doJSON(ctx, http.MethodGet, path, nil, nil, nil, &fullRemote); err != nil {
				return agentruntime.Memory{}, err
			}
			mapped = mapQoderMemory(binding.OMAExternalID, fullRemote, "full")
		}
	}
	return mapped, nil
}

func (r *Runtime) DeleteMemory(ctx context.Context, scope agentruntime.Scope, storeID string, memoryID string, input agentruntime.DeleteMemoryInput) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return err
	}
	params := url.Values{}
	if input.ExpectedContentSHA256 != nil && *input.ExpectedContentSHA256 != "" {
		params.Set("expected_content_sha256", *input.ExpectedContentSHA256)
	}
	path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memories/" + url.PathEscape(memoryID)
	return r.doJSON(ctx, http.MethodDelete, path, params, nil, nil, nil)
}

func (r *Runtime) ListMemoryVersions(ctx context.Context, scope agentruntime.Scope, storeID string, query agentruntime.ListMemoryVersionsQuery) (agentruntime.Page[agentruntime.MemoryVersion], error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.Page[agentruntime.MemoryVersion]{}, err
	}
	params := buildMemoryVersionsQueryParams(query)
	var remotePage qoderMemoryVersionsPage
	if err := r.doJSON(ctx, http.MethodGet, "/memory_stores/"+url.PathEscape(binding.RemoteID)+"/memory_versions", params, nil, nil, &remotePage); err != nil {
		return agentruntime.Page[agentruntime.MemoryVersion]{}, err
	}
	items := make([]agentruntime.MemoryVersion, 0, len(remotePage.Data))
	for _, entry := range remotePage.Data {
		items = append(items, mapQoderMemoryVersion(binding.OMAExternalID, entry, query.View))
	}
	if query.View == "full" {
		if err := r.hydrateVersionsContent(ctx, binding, items); err != nil {
			return agentruntime.Page[agentruntime.MemoryVersion]{}, err
		}
	}
	nextCursor := resolveNextPageCursor(remotePage.NextPage, remotePage.HasMore, remotePage.LastID)
	page := agentruntime.Page[agentruntime.MemoryVersion]{
		Items:      items,
		FirstID:    remotePage.FirstID,
		LastID:     remotePage.LastID,
		HasMore:    remotePage.HasMore || nextCursor != "",
		NextCursor: nextCursor,
	}
	if len(items) > 0 {
		if page.FirstID == "" {
			page.FirstID = items[0].ID
		}
		if page.LastID == "" {
			page.LastID = items[len(items)-1].ID
		}
	}
	return page, nil
}

func (r *Runtime) hydrateVersionsContent(ctx context.Context, binding db.RuntimeResourceBinding, items []agentruntime.MemoryVersion) error {
	if len(items) == 0 {
		return nil
	}
	hydrateCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, memoryHydrateConcurrency)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for idx := range items {
		if items[idx].Content != nil || items[idx].Redacted || items[idx].Operation == "deleted" {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if hydrateCtx.Err() != nil {
				return
			}
			var remote qoderMemoryVersion
			path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memory_versions/" + url.PathEscape(items[i].ID)
			if err := r.doJSON(hydrateCtx, http.MethodGet, path, nil, nil, nil, &remote); err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			items[i] = mapQoderMemoryVersion(binding.OMAExternalID, remote, "full")
		}(idx)
	}
	wg.Wait()
	return firstErr
}

func (r *Runtime) GetMemoryVersion(ctx context.Context, scope agentruntime.Scope, storeID string, versionID string, query agentruntime.GetMemoryVersionQuery) (agentruntime.MemoryVersion, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.MemoryVersion{}, err
	}
	var remote qoderMemoryVersion
	path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memory_versions/" + url.PathEscape(versionID)
	if err := r.doJSON(ctx, http.MethodGet, path, nil, nil, nil, &remote); err != nil {
		return agentruntime.MemoryVersion{}, err
	}
	view := query.View
	if view == "" {
		view = "full"
	}
	return mapQoderMemoryVersion(binding.OMAExternalID, remote, view), nil
}

func (r *Runtime) RedactMemoryVersion(ctx context.Context, scope agentruntime.Scope, storeID string, versionID string) (agentruntime.MemoryVersion, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, storeID)
	if err != nil {
		return agentruntime.MemoryVersion{}, err
	}
	var remote qoderMemoryVersion
	path := "/memory_stores/" + url.PathEscape(binding.RemoteID) + "/memory_versions/" + url.PathEscape(versionID) + "/redact"
	if err := r.doJSON(ctx, http.MethodPost, path, nil, nil, nil, &remote); err != nil {
		return agentruntime.MemoryVersion{}, err
	}
	return mapQoderMemoryVersion(binding.OMAExternalID, remote, "basic"), nil
}

func buildMemoryVersionsQueryParams(query agentruntime.ListMemoryVersionsQuery) url.Values {
	params := url.Values{}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	if query.Page != "" {
		params.Set("page", query.Page)
	}
	if query.MemoryID != "" {
		params.Set("memory_id", query.MemoryID)
	}
	if query.Path != "" {
		params.Set("path", toQoderMemoryPath(query.Path))
	}
	if query.Operation != "" {
		params.Set("operation", query.Operation)
	}
	if query.APIKeyID != "" {
		params.Set("api_key_id", query.APIKeyID)
	}
	if query.SessionID != "" {
		params.Set("session_id", query.SessionID)
	}
	if query.CreatedAtGTE != nil {
		params.Set("created_at[gte]", query.CreatedAtGTE.UTC().Format(time.RFC3339))
	}
	if query.CreatedAtLTE != nil {
		params.Set("created_at[lte]", query.CreatedAtLTE.UTC().Format(time.RFC3339))
	}
	return params
}

func isQoderStoreArchived(remote qoderMemoryStore) bool {
	return remote.ArchivedAt != nil || remote.Status == "archived"
}

func mapQoderMemoryStore(omaID string, remote qoderMemoryStore) agentruntime.MemoryStore {
	archivedAt := remote.ArchivedAt
	if archivedAt == nil && remote.Status == "archived" {
		ts := remote.UpdatedAt
		archivedAt = &ts
	}
	return agentruntime.MemoryStore{
		ID:          omaID,
		Name:        remote.Name,
		Description: remote.Description,
		Metadata:    stripWatermark(remote.Metadata),
		ArchivedAt:  archivedAt,
		CreatedAt:   remote.CreatedAt,
		UpdatedAt:   remote.UpdatedAt,
	}
}

func memoryStoreFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.MemoryStore {
	var summary memoryStoreBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	store := agentruntime.MemoryStore{
		ID:          row.OMAExternalID,
		Name:        summary.Name,
		Description: summary.Description,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.State == "archived" || summary.Archived {
		ts := row.UpdatedAt
		store.ArchivedAt = &ts
	}
	return store
}

func mapQoderMemory(omaStoreID string, remote qoderMemory, view string) agentruntime.Memory {
	versionID := remote.MemoryVersionID
	if versionID == "" {
		versionID = string(remote.Version)
	}
	var content *string
	if view == "full" && remote.Content != nil {
		val := *remote.Content
		content = &val
	}
	return agentruntime.Memory{
		ID:               remote.ID,
		MemoryStoreID:    omaStoreID,
		MemoryVersionID:  versionID,
		Path:             toOMAMemoryPath(remote.Path),
		Content:          content,
		ContentSizeBytes: remote.ContentSizeBytes,
		ContentSHA256:    remote.ContentSHA256,
		CreatedAt:        remote.CreatedAt,
		UpdatedAt:        remote.UpdatedAt,
	}
}

func mapQoderMemoryVersion(omaStoreID string, remote qoderMemoryVersion, view string) agentruntime.MemoryVersion {
	var path *string
	if remote.Path != nil && *remote.Path != "" {
		normalized := toOMAMemoryPath(*remote.Path)
		path = &normalized
	}
	redacted := remote.Redacted || remote.RedactedAt != nil
	var content *string
	if view == "full" && !redacted && remote.Operation != "deleted" && remote.Content != nil {
		val := *remote.Content
		content = &val
	}
	createdBy := agentruntime.MemoryActor{Type: "api_actor"}
	if remote.CreatedBy != nil && remote.CreatedBy.Type != "" {
		createdBy = agentruntime.MemoryActor{
			Type:      remote.CreatedBy.Type,
			APIKeyID:  remote.CreatedBy.APIKeyID,
			SessionID: remote.CreatedBy.SessionID,
			UserID:    remote.CreatedBy.UserID,
		}
	}
	var redactedBy *agentruntime.MemoryActor
	if remote.RedactedBy != nil && remote.RedactedBy.Type != "" {
		redactedBy = &agentruntime.MemoryActor{
			Type:      remote.RedactedBy.Type,
			APIKeyID:  remote.RedactedBy.APIKeyID,
			SessionID: remote.RedactedBy.SessionID,
			UserID:    remote.RedactedBy.UserID,
		}
	}
	return agentruntime.MemoryVersion{
		ID:               remote.ID,
		MemoryID:         remote.MemoryID,
		MemoryStoreID:    omaStoreID,
		Operation:        remote.Operation,
		Path:             path,
		Content:          content,
		ContentSizeBytes: remote.ContentSizeBytes,
		ContentSHA256:    remote.ContentSHA256,
		CreatedBy:        createdBy,
		Redacted:         redacted,
		RedactedAt:       remote.RedactedAt,
		RedactedBy:       redactedBy,
		CreatedAt:        remote.CreatedAt,
	}
}

func toQoderPrecondition(pre *agentruntime.MemoryPrecondition) *qoderMemoryPrecondition {
	if pre == nil {
		return nil
	}
	return &qoderMemoryPrecondition{
		Type:          pre.Type,
		ContentSHA256: pre.ContentSHA256,
	}
}

func toQoderMemoryPath(path string) string {
	return strings.TrimLeft(path, "/")
}

func toQoderMemoryPathPrefix(prefix string) string {
	return strings.TrimLeft(prefix, "/")
}

func toOMAMemoryPath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func normalizePrefixPath(prefix string) string {
	normalized := toOMAMemoryPath(prefix)
	if normalized == "" {
		return ""
	}
	if !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}
	return normalized
}

func resolveNextPageCursor(nextPage *string, hasMore bool, lastID string) string {
	if nextPage != nil && *nextPage != "" {
		return *nextPage
	}
	if hasMore && lastID != "" {
		return lastID
	}
	return ""
}

func foldMemoriesForDepth(memories []agentruntime.Memory, upstreamPrefixes []string, pathPrefix string, depth int) []foldedMemoryEntry {
	seenPrefixes := make(map[string]struct{})
	entries := make([]foldedMemoryEntry, 0, len(memories)+len(upstreamPrefixes))
	for _, prefix := range upstreamPrefixes {
		if _, seen := seenPrefixes[prefix]; seen {
			continue
		}
		seenPrefixes[prefix] = struct{}{}
		entries = append(entries, foldedMemoryEntry{Path: prefix, Prefix: prefix})
	}
	for i := range memories {
		mem := memories[i]
		if prefix, ok := rollupMemoryPrefix(mem.Path, pathPrefix, depth); ok {
			if _, seen := seenPrefixes[prefix]; seen {
				continue
			}
			seenPrefixes[prefix] = struct{}{}
			entries = append(entries, foldedMemoryEntry{Path: prefix, Prefix: prefix})
			continue
		}
		entries = append(entries, foldedMemoryEntry{Path: mem.Path, Memory: &mem})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func rollupMemoryPrefix(path, pathPrefix string, depth int) (string, bool) {
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
