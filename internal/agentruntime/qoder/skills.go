package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const skillHydrateConcurrency = 5

type qoderSkill struct {
	ID            string    `json:"id"`
	DisplayTitle  string    `json:"display_title"`
	Description   string    `json:"description"`
	LatestVersion string    `json:"latest_version"`
	Source        string    `json:"source"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type qoderSkillsEnvelope struct {
	Data     []qoderSkill `json:"data"`
	Skills   []qoderSkill `json:"skills"`
	NextPage *string      `json:"next_page"`
}

type qoderSkillVersion struct {
	ID          string    `json:"id"`
	SkillID     string    `json:"skill_id"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Directory   string    `json:"directory"`
	CreatedAt   time.Time `json:"created_at"`
}

type qoderSkillVersionsEnvelope struct {
	Data     []qoderSkillVersion `json:"data"`
	Versions []qoderSkillVersion `json:"versions"`
	NextPage *string             `json:"next_page"`
}

type skillBindingSummary struct {
	DisplayTitle  string `json:"display_title,omitempty"`
	Description   string `json:"description,omitempty"`
	IconKey       string `json:"icon_key,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
	Source        string `json:"source,omitempty"`
}

func (r *Runtime) CreateSkill(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateSkillInput) (agentruntime.Skill, error) {
	if len(input.Files) == 0 {
		return agentruntime.Skill{}, agentruntime.InvalidArgumentError("skill files are required", nil)
	}
	var remote qoderSkill
	if err := r.postSkillMultipart(ctx, "/skills", input.DisplayTitle, input.Files, &remote); err != nil {
		return agentruntime.Skill{}, err
	}
	omaID, err := ids.New("skill_")
	if err != nil {
		return agentruntime.Skill{}, agentruntime.InvalidArgumentError("failed to generate skill id", err)
	}
	mapped := mapQoderSkill(omaID, remote, input.IconKey, input.Description)
	summary, _ := json.Marshal(skillBindingSummary{
		DisplayTitle:  mapped.DisplayTitle,
		Description:   mapped.Description,
		IconKey:       mapped.IconKey,
		LatestVersion: mapped.LatestVersion,
		Source:        mapped.Source,
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeSkill,
		OMAExternalID: omaID,
		RemoteID:      remote.ID,
	}, summary); err != nil {
		return agentruntime.Skill{}, err
	}
	return mapped, nil
}

func (r *Runtime) GetSkill(ctx context.Context, scope agentruntime.Scope, skillID string) (agentruntime.Skill, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSkill, skillID)
	if err == nil {
		var remote qoderSkill
		if getErr := r.doJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); getErr != nil {
			return agentruntime.Skill{}, getErr
		}
		var sum skillBindingSummary
		_ = json.Unmarshal(binding.Summary, &sum)
		mapped := mapQoderSkill(binding.OMAExternalID, remote, sum.IconKey, sum.Description)
		r.refreshSkillBinding(ctx, scope, binding, mapped)
		return mapped, nil
	}
	if !errors.Is(err, agentruntime.ErrResourceNotFound) {
		return agentruntime.Skill{}, err
	}
	var remote qoderSkill
	if getErr := r.doJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(skillID), nil, nil, nil, &remote); getErr != nil {
		return agentruntime.Skill{}, getErr
	}
	if isCustomSkillSource(remote.Source) {
		return agentruntime.Skill{}, agentruntime.NotFoundError("skill not found", nil)
	}
	return mapQoderSkill(remote.ID, remote, "", ""), nil
}

func (r *Runtime) ListSkills(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListSkillsQuery) (agentruntime.Page[agentruntime.Skill], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	var items []agentruntime.Skill
	if query.Source == "" || query.Source == "anthropic" {
		builtins, err := r.fetchBuiltinSkills(ctx)
		if err != nil {
			return agentruntime.Page[agentruntime.Skill]{}, err
		}
		items = append(items, builtins...)
	}
	if query.Source == "" || query.Source == "custom" {
		rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeSkill, "", "", false, 1000, 0)
		if err != nil {
			return agentruntime.Page[agentruntime.Skill]{}, err
		}
		items = append(items, r.hydrateSkills(ctx, rows)...)
	}
	if offset >= len(items) {
		return agentruntime.Page[agentruntime.Skill]{Items: []agentruntime.Skill{}}, nil
	}
	end := offset + limit
	hasMore := end < len(items)
	if end > len(items) {
		end = len(items)
	}
	slice := items[offset:end]
	page := agentruntime.Page[agentruntime.Skill]{
		Items:   slice,
		HasMore: hasMore,
	}
	if len(slice) > 0 {
		page.FirstID = slice[0].ID
		page.LastID = slice[len(slice)-1].ID
	}
	return page, nil
}

func (r *Runtime) DeleteSkill(ctx context.Context, scope agentruntime.Scope, skillID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSkill, skillID)
	if err != nil {
		return err
	}
	if err := r.doJSON(ctx, http.MethodDelete, "/skills/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil); err != nil {
		return err
	}
	return r.bindings.SoftDelete(ctx, scope, agentruntime.ResourceTypeSkill, binding.OMAExternalID)
}

func (r *Runtime) CreateSkillVersion(ctx context.Context, scope agentruntime.Scope, skillID string, input agentruntime.CreateSkillVersionInput) (agentruntime.SkillVersion, error) {
	if len(input.Files) == 0 {
		return agentruntime.SkillVersion{}, agentruntime.InvalidArgumentError("skill files are required", nil)
	}
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSkill, skillID)
	if err != nil {
		return agentruntime.SkillVersion{}, err
	}
	var remote qoderSkillVersion
	if err := r.postSkillMultipart(ctx, "/skills/"+url.PathEscape(binding.RemoteID)+"/versions", "", input.Files, &remote); err != nil {
		return agentruntime.SkillVersion{}, err
	}
	var sum skillBindingSummary
	_ = json.Unmarshal(binding.Summary, &sum)
	if input.IconKey != "" {
		sum.IconKey = input.IconKey
	}
	if input.Description != "" {
		sum.Description = input.Description
	} else if remote.Description != "" {
		sum.Description = remote.Description
	}
	if remote.Version != "" {
		sum.LatestVersion = remote.Version
	}
	updatedSummary, _ := json.Marshal(sum)
	_, _ = r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeSkill, binding.OMAExternalID, "active", updatedSummary)
	return mapQoderSkillVersion(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ListSkillVersions(ctx context.Context, scope agentruntime.Scope, skillID string, query agentruntime.ListSkillVersionsQuery) (agentruntime.Page[agentruntime.SkillVersion], error) {
	remoteSkillID, omaSkillID, err := r.resolveReadableSkillID(ctx, scope, skillID)
	if err != nil {
		return agentruntime.Page[agentruntime.SkillVersion]{}, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := normalizeListOffset(query.Offset)
	collected := make([]agentruntime.SkillVersion, 0)
	pageToken := ""
	for pageCount := 0; pageCount < 20; pageCount++ {
		params := url.Values{}
		params.Set("limit", "100")
		if pageToken != "" {
			params.Set("page", pageToken)
		}
		var env qoderSkillVersionsEnvelope
		if err := r.doJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(remoteSkillID)+"/versions", params, nil, nil, &env); err != nil {
			return agentruntime.Page[agentruntime.SkillVersion]{}, err
		}
		raw := env.Data
		if len(raw) == 0 {
			raw = env.Versions
		}
		for _, item := range raw {
			collected = append(collected, mapQoderSkillVersion(omaSkillID, item))
		}
		if env.NextPage == nil || *env.NextPage == "" {
			break
		}
		pageToken = *env.NextPage
	}
	sort.Slice(collected, func(i, j int) bool {
		if collected[i].CreatedAt.Equal(collected[j].CreatedAt) {
			return collected[i].Version > collected[j].Version
		}
		return collected[i].CreatedAt.After(collected[j].CreatedAt)
	})
	if offset >= len(collected) {
		return agentruntime.Page[agentruntime.SkillVersion]{Items: []agentruntime.SkillVersion{}}, nil
	}
	end := offset + limit
	hasMore := end < len(collected)
	if end > len(collected) {
		end = len(collected)
	}
	slice := collected[offset:end]
	page := agentruntime.Page[agentruntime.SkillVersion]{
		Items:   slice,
		HasMore: hasMore,
	}
	if len(slice) > 0 {
		page.FirstID = slice[0].Version
		page.LastID = slice[len(slice)-1].Version
	}
	return page, nil
}

func (r *Runtime) GetSkillVersion(ctx context.Context, scope agentruntime.Scope, skillID string, version string) (agentruntime.SkillVersion, error) {
	remoteSkillID, omaSkillID, err := r.resolveReadableSkillID(ctx, scope, skillID)
	if err != nil {
		return agentruntime.SkillVersion{}, err
	}
	targetVersion := version
	if targetVersion == "latest" {
		skill, err := r.GetSkill(ctx, scope, skillID)
		if err != nil {
			return agentruntime.SkillVersion{}, err
		}
		if skill.LatestVersion == "" {
			return agentruntime.SkillVersion{}, agentruntime.NotFoundError("skill version not found", nil)
		}
		targetVersion = skill.LatestVersion
	}
	var remote qoderSkillVersion
	if err := r.doJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(remoteSkillID)+"/versions/"+url.PathEscape(targetVersion), nil, nil, nil, &remote); err != nil {
		return agentruntime.SkillVersion{}, err
	}
	return mapQoderSkillVersion(omaSkillID, remote), nil
}

func (r *Runtime) DeleteSkillVersion(ctx context.Context, scope agentruntime.Scope, skillID string, version string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSkill, skillID)
	if err != nil {
		return err
	}
	return r.doJSON(ctx, http.MethodDelete, "/skills/"+url.PathEscape(binding.RemoteID)+"/versions/"+url.PathEscape(version), nil, nil, nil, nil)
}

func (r *Runtime) resolveReadableSkillID(ctx context.Context, scope agentruntime.Scope, skillID string) (string, string, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSkill, skillID)
	if err == nil {
		return binding.RemoteID, binding.OMAExternalID, nil
	}
	if !errors.Is(err, agentruntime.ErrResourceNotFound) {
		return "", "", err
	}
	var remote qoderSkill
	if getErr := r.doJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(skillID), nil, nil, nil, &remote); getErr != nil {
		return "", "", getErr
	}
	if isCustomSkillSource(remote.Source) {
		return "", "", agentruntime.NotFoundError("skill not found", nil)
	}
	return remote.ID, remote.ID, nil
}

func (r *Runtime) fetchBuiltinSkills(ctx context.Context) ([]agentruntime.Skill, error) {
	params := url.Values{}
	params.Set("source", "anthropic")
	params.Set("limit", "100")
	var env qoderSkillsEnvelope
	if err := r.doJSON(ctx, http.MethodGet, "/skills", params, nil, nil, &env); err != nil {
		return nil, err
	}
	raw := env.Data
	if len(raw) == 0 {
		raw = env.Skills
	}
	out := make([]agentruntime.Skill, 0, len(raw))
	for _, item := range raw {
		if isCustomSkillSource(item.Source) {
			continue
		}
		out = append(out, mapQoderSkill(item.ID, item, "", ""))
	}
	return out, nil
}

func (r *Runtime) hydrateSkills(ctx context.Context, rows []db.RuntimeResourceBinding) []agentruntime.Skill {
	if len(rows) == 0 {
		return nil
	}
	items := make([]agentruntime.Skill, len(rows))
	sem := make(chan struct{}, skillHydrateConcurrency)
	var wg sync.WaitGroup
	for idx, row := range rows {
		wg.Add(1)
		go func(i int, binding db.RuntimeResourceBinding) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var sum skillBindingSummary
			_ = json.Unmarshal(binding.Summary, &sum)
			var remote qoderSkill
			if err := r.doJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
				r.logger.WarnContext(ctx, "qoder skill hydrate failed, falling back to binding summary", "oma_id", binding.OMAExternalID, "error", err)
				items[i] = skillFromBindingSummary(binding, sum)
				return
			}
			items[i] = mapQoderSkill(binding.OMAExternalID, remote, sum.IconKey, sum.Description)
		}(idx, row)
	}
	wg.Wait()
	return items
}

func (r *Runtime) postSkillMultipart(
	ctx context.Context,
	path string,
	displayTitle string,
	files []agentruntime.SkillFile,
	respOut any,
) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if displayTitle != "" {
		if err := writer.WriteField("display_title", displayTitle); err != nil {
			return agentruntime.InvalidArgumentError("failed to encode display_title", err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile("files", file.Path)
		if err != nil {
			return agentruntime.InvalidArgumentError("failed to encode skill file", err)
		}
		if _, err := part.Write(file.Data); err != nil {
			return agentruntime.InvalidArgumentError("failed to write skill file", err)
		}
	}
	if err := writer.Close(); err != nil {
		return agentruntime.InvalidArgumentError("failed to finalize skill multipart body", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.buildURL(path, nil), &buf)
	if err != nil {
		return upstreamTransportError(err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := r.restClient.Do(req)
	if err != nil {
		r.logger.WarnContext(ctx, "qoder skill upload failed", "path", path, "error", err)
		return upstreamTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readAndMapUpstreamError(resp)
	}
	if respOut == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes))
	if err != nil {
		return upstreamTransportError(err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, respOut); err != nil {
		return upstreamTransportError(err)
	}
	return nil
}

func (r *Runtime) translateAgentSkillsToQoder(ctx context.Context, scope agentruntime.Scope, raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw, nil
	}
	if len(items) == 0 {
		return raw, nil
	}
	changed := false
	for _, item := range items {
		var kind string
		if rawType, ok := item["type"]; ok {
			_ = json.Unmarshal(rawType, &kind)
		}
		if kind != "" && kind != "custom" {
			continue
		}
		var skillID string
		if rawID, ok := item["skill_id"]; ok {
			_ = json.Unmarshal(rawID, &skillID)
		}
		if skillID == "" {
			continue
		}
		binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSkill, skillID)
		if err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(binding.RemoteID)
		item["skill_id"] = encoded
		changed = true
	}
	if !changed {
		return raw, nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, agentruntime.InvalidArgumentError("failed to encode translated agent skills", err)
	}
	return json.RawMessage(encoded), nil
}

func (r *Runtime) translateAgentSkillsFromQoder(ctx context.Context, scope agentruntime.Scope, raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return raw
	}
	changed := false
	for _, item := range items {
		var skillID string
		if rawID, ok := item["skill_id"]; ok {
			_ = json.Unmarshal(rawID, &skillID)
		}
		if skillID == "" {
			continue
		}
		if binding, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeSkill, skillID); err == nil {
			encoded, _ := json.Marshal(binding.OMAExternalID)
			item["skill_id"] = encoded
			changed = true
		}
	}
	if !changed {
		return raw
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return json.RawMessage(encoded)
}

func (r *Runtime) refreshSkillBinding(ctx context.Context, scope agentruntime.Scope, binding db.RuntimeResourceBinding, mapped agentruntime.Skill) {
	summary, _ := json.Marshal(skillBindingSummary{
		DisplayTitle:  mapped.DisplayTitle,
		Description:   mapped.Description,
		IconKey:       mapped.IconKey,
		LatestVersion: mapped.LatestVersion,
		Source:        mapped.Source,
	})
	_, _ = r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeSkill, binding.OMAExternalID, "active", summary)
}

func isCustomSkillSource(source string) bool {
	return source == "" || source == "custom"
}

func mapQoderSkill(omaID string, remote qoderSkill, iconKey, fallbackDesc string) agentruntime.Skill {
	source := remote.Source
	if source == "" {
		source = "custom"
	}
	desc := remote.Description
	if desc == "" {
		desc = fallbackDesc
	}
	updatedAt := remote.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = remote.CreatedAt
	}
	return agentruntime.Skill{
		ID:            omaID,
		DisplayTitle:  remote.DisplayTitle,
		Description:   desc,
		IconKey:       iconKey,
		LatestVersion: remote.LatestVersion,
		Source:        source,
		CreatedAt:     remote.CreatedAt,
		UpdatedAt:     updatedAt,
	}
}

func skillFromBindingSummary(row db.RuntimeResourceBinding, sum skillBindingSummary) agentruntime.Skill {
	source := sum.Source
	if source == "" {
		source = "custom"
	}
	return agentruntime.Skill{
		ID:            row.OMAExternalID,
		DisplayTitle:  sum.DisplayTitle,
		Description:   sum.Description,
		IconKey:       sum.IconKey,
		LatestVersion: sum.LatestVersion,
		Source:        source,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func mapQoderSkillVersion(omaSkillID string, remote qoderSkillVersion) agentruntime.SkillVersion {
	return agentruntime.SkillVersion{
		ID:          remote.ID,
		SkillID:     omaSkillID,
		Version:     remote.Version,
		Name:        remote.Name,
		Description: remote.Description,
		Directory:   remote.Directory,
		CreatedAt:   remote.CreatedAt,
	}
}
