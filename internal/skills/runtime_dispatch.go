package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) WithRuntimeResolver(resolver *agentruntime.Resolver) *Handler {
	h.runtimeResolver = resolver
	return h
}

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func (h *Handler) resolveExternalSkillRuntime(
	ctx context.Context,
	principal auth.Principal,
) (agentruntime.Runtime, agentruntime.SkillRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, skillRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	skillRT, ok := rt.(agentruntime.SkillRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, skillRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, skillRT, scope, nil
}

func (h *Handler) createExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	pkg, err := readSkillPackage(w, r, MaxSkillPackageBytes)
	if err != nil {
		return mapSkillPackageError(err)
	}
	iconKey, err := skillIconKey(r.FormValue("icon_key"))
	if err != nil {
		return err
	}
	displayTitle, err := skillDisplayLabel(r, pkg)
	if err != nil {
		return err
	}
	files, err := extractSkillFilesFromZip(pkg.Zip)
	if err != nil {
		return internalError("Could not read skill archive", err)
	}
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := skillRT.CreateSkill(r.Context(), scope, agentruntime.CreateSkillInput{
		DisplayTitle: displayTitle,
		IconKey:      iconKey,
		Description:  pkg.Description,
		Files:        files,
	})
	if err != nil {
		return err
	}
	return h.writeSkill(w, r, responseFromExternalSkill(created))
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if !hasSkillsBeta(r) && source == "anthropic_example" {
		source = "anthropic"
	}
	if source != "" && source != "custom" && source != "anthropic" {
		return h.writeSkillsPage(w, r, pageResponse[skillResponse]{Data: []skillResponse{}})
	}
	maximum := maxSkillsLimit
	if !hasSkillsBeta(r) {
		maximum = 1000
	}
	limit, err := parseLimitParam(r, defaultSkillsLimit, maximum)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"), hasSkillsBeta(r))
	if err != nil {
		return invalidRequest(err)
	}
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := skillRT.ListSkills(r.Context(), scope, agentruntime.ListSkillsQuery{
		Source: source,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return err
	}
	data := make([]skillResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromExternalSkill(item))
	}
	var nextPage *string
	if page.HasMore {
		value := encodePageOffset(offset+len(data), hasSkillsBeta(r))
		nextPage = &value
	}
	return h.writeSkillsPage(w, r, pageResponse[skillResponse]{Data: data, HasMore: page.HasMore, NextPage: nextPage})
}

func (h *Handler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, skillID string) error {
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	item, err := skillRT.GetSkill(r.Context(), scope, skillID)
	if err != nil {
		return err
	}
	return h.writeSkill(w, r, responseFromExternalSkill(item))
}

func (h *Handler) deleteExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, skillID string) error {
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := skillRT.DeleteSkill(r.Context(), scope, skillID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": skillID, "type": "skill_deleted"})
	return nil
}

func (h *Handler) createVersionExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, skillID string) error {
	pkg, err := readSkillPackage(w, r, MaxSkillPackageBytes)
	if err != nil {
		return mapSkillPackageError(err)
	}
	iconKey := ""
	if r.FormValue("icon_key") != "" {
		key, iconErr := skillIconKey(r.FormValue("icon_key"))
		if iconErr != nil {
			return iconErr
		}
		iconKey = key
	}
	files, err := extractSkillFilesFromZip(pkg.Zip)
	if err != nil {
		return internalError("Could not read skill archive", err)
	}
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	version, err := skillRT.CreateSkillVersion(r.Context(), scope, skillID, agentruntime.CreateSkillVersionInput{
		IconKey:     iconKey,
		Description: pkg.Description,
		Files:       files,
	})
	if err != nil {
		return err
	}
	writeVersion(w, r, responseFromExternalSkillVersion(version))
	return nil
}

func (h *Handler) listVersionsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, skillID string) error {
	limit, err := parseLimitParam(r, defaultSkillVersionsLimit, maxSkillVersionsLimit)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"), hasSkillsBeta(r))
	if err != nil {
		return invalidRequest(err)
	}
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := skillRT.ListSkillVersions(r.Context(), scope, skillID, agentruntime.ListSkillVersionsQuery{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return err
	}
	data := make([]skillVersionResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromExternalSkillVersion(item))
	}
	var nextPage *string
	if page.HasMore {
		value := encodePageOffset(offset+len(data), hasSkillsBeta(r))
		nextPage = &value
	}
	writeVersionsPage(w, r, pageResponse[skillVersionResponse]{
		Data:     data,
		HasMore:  page.HasMore,
		NextPage: nextPage,
	})
	return nil
}

func (h *Handler) retrieveVersionExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, skillID, version string) error {
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	item, err := skillRT.GetSkillVersion(r.Context(), scope, skillID, version)
	if err != nil {
		return err
	}
	writeVersion(w, r, responseFromExternalSkillVersion(item))
	return nil
}

func (h *Handler) deleteVersionExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, skillID, version string) error {
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := skillRT.DeleteSkillVersion(r.Context(), scope, skillID, version); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": version, "type": "skill_version_deleted"})
	return nil
}

func (h *Handler) consoleCatalogExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, limit, offset int) error {
	searchQuery := strings.TrimSpace(r.URL.Query().Get("query"))
	source := r.URL.Query().Get("source")
	sortBy := r.URL.Query().Get("sort")
	if len(searchQuery) > 256 || (source != "" && source != "custom" && source != "anthropic") || (sortBy != "" && sortBy != "name" && sortBy != "updated") {
		return errInvalidSkillFilter
	}
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := skillRT.ListSkills(r.Context(), scope, agentruntime.ListSkillsQuery{
		Limit:  1000,
		Offset: 0,
	})
	if err != nil {
		return err
	}
	needle := strings.ToLower(searchQuery)
	counts := consoleSkillCounts{}
	filtered := make([]consoleCatalogSkill, 0, len(page.Items))
	for _, item := range page.Items {
		if needle != "" &&
			!strings.Contains(strings.ToLower(item.DisplayTitle), needle) &&
			!strings.Contains(strings.ToLower(item.Description), needle) &&
			!strings.Contains(strings.ToLower(item.ID), needle) {
			continue
		}
		counts.All++
		if item.Source == "anthropic" {
			counts.Builtin++
		} else {
			counts.Custom++
		}
		if source != "" && item.Source != source {
			continue
		}
		filtered = append(filtered, consoleCatalogSkill{
			skillResponse: responseFromExternalSkill(item),
			Description:   item.Description,
		})
	}
	if sortBy == "name" {
		sort.Slice(filtered, func(i, j int) bool {
			return strings.ToLower(filtered[i].DisplayTitle) < strings.ToLower(filtered[j].DisplayTitle)
		})
	} else {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].UpdatedAt > filtered[j].UpdatedAt
		})
	}
	total := len(filtered)
	if offset >= total {
		filtered = []consoleCatalogSkill{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		filtered = filtered[offset:end]
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Data       []consoleCatalogSkill `json:"data"`
		TotalCount int                   `json:"total_count"`
		Counts     consoleSkillCounts    `json:"counts"`
	}{filtered, total, counts})
	return nil
}

func (h *Handler) inspectConsolePackageExternal(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	targetSkillID string,
	pkg skillPackage,
	preview systemPackageResponse,
) error {
	rt, skillRT, scope, err := h.resolveExternalSkillRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	skill, err := skillRT.GetSkill(r.Context(), scope, targetSkillID)
	if err != nil {
		return err
	}
	if skill.LatestVersion != "" {
		previous, err := skillRT.GetSkillVersion(r.Context(), scope, targetSkillID, skill.LatestVersion)
		if err != nil {
			return err
		}
		if previous.Name != "" && previous.Name != pkg.Name {
			return skillNameImmutable()
		}
	}
	if skill.DisplayTitle != "" {
		preview.Name = skill.DisplayTitle
	}
	httpapi.WriteJSON(w, http.StatusOK, preview)
	return nil
}

func extractSkillFilesFromZip(zipBytes []byte) ([]agentruntime.SkillFile, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	files := make([]agentruntime.SkillFile, 0, len(reader.File))
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		files = append(files, agentruntime.SkillFile{
			Path: entry.Name,
			Data: data,
		})
	}
	return files, nil
}

func responseFromExternalSkill(item agentruntime.Skill) skillResponse {
	return skillResponse{
		ID:            item.ID,
		CreatedAt:     formatTime(item.CreatedAt),
		DisplayTitle:  item.DisplayTitle,
		IconKey:       item.IconKey,
		LatestVersion: item.LatestVersion,
		Source:        item.Source,
		Type:          "skill",
		UpdatedAt:     formatTime(item.UpdatedAt),
	}
}

func responseFromExternalSkillVersion(item agentruntime.SkillVersion) skillVersionResponse {
	return skillVersionResponse{
		ID:          item.ID,
		CreatedAt:   formatTime(item.CreatedAt),
		Description: item.Description,
		Directory:   item.Directory,
		Name:        item.Name,
		SkillID:     item.SkillID,
		Type:        "skill_version",
		Version:     item.Version,
	}
}
