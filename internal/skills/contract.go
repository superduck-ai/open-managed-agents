package skills

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type skillSource struct {
	Type string `json:"type"`
}
type stableSkillResponse struct {
	ID              string      `json:"id"`
	CreatedAt       string      `json:"created_at"`
	DisplayName     string      `json:"display_name"`
	LatestVersionID string      `json:"latest_version_id"`
	Source          skillSource `json:"source"`
	Type            string      `json:"type"`
	UpdatedAt       string      `json:"updated_at"`
}
type stableVersionResponse struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"created_at"`
	Description string `json:"description"`
	Name        string `json:"name"`
	SkillID     string `json:"skill_id"`
	Type        string `json:"type"`
}
type stablePageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

func (h *Handler) stableSkill(r *http.Request, skill skillResponse) (stableSkillResponse, error) {
	if skill.LatestVersion == "" {
		return stableSkillResponse{}, skillHasNoVersions()
	}
	var versionID string
	if skill.Source == "anthropic" {
		version, err := h.db.GetBuiltinSkillVersion(r.Context(), skill.ID, skill.LatestVersion)
		if err != nil {
			return stableSkillResponse{}, internalError("Could not load latest skill version", err)
		}
		versionID = version.ExternalID
	} else {
		principal, _ := auth.PrincipalFromContext(r.Context())
		version, err := h.db.GetSkillVersion(r.Context(), principal.WorkspaceUUID, skill.ID, skill.LatestVersion)
		if err != nil {
			return stableSkillResponse{}, internalError("Could not load latest skill version", err)
		}
		versionID = version.ExternalID
	}
	source := skill.Source
	if source == "anthropic" {
		source = "anthropic_example"
	}
	return stableSkillResponse{ID: skill.ID, CreatedAt: skill.CreatedAt, DisplayName: skill.DisplayTitle, LatestVersionID: versionID, Source: skillSource{Type: source}, Type: skill.Type, UpdatedAt: skill.UpdatedAt}, nil
}

func (h *Handler) writeSkill(w http.ResponseWriter, r *http.Request, skill skillResponse) error {
	if hasSkillsBeta(r) {
		httpapi.WriteJSON(w, http.StatusOK, skill)
		return nil
	}
	response, err := h.stableSkill(r, skill)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) writeSkillsPage(w http.ResponseWriter, r *http.Request, page pageResponse[skillResponse]) error {
	if hasSkillsBeta(r) {
		httpapi.WriteJSON(w, http.StatusOK, page)
		return nil
	}
	data := make([]stableSkillResponse, 0, len(page.Data))
	for _, skill := range page.Data {
		response, err := h.stableSkill(r, skill)
		if err != nil {
			return err
		}
		data = append(data, response)
	}
	httpapi.WriteJSON(w, http.StatusOK, stablePageResponse[stableSkillResponse]{Data: data, NextPage: page.NextPage})
	return nil
}

func stableVersion(version skillVersionResponse) stableVersionResponse {
	return stableVersionResponse{ID: version.ID, CreatedAt: version.CreatedAt, Description: version.Description, Name: version.Name, SkillID: version.SkillID, Type: version.Type}
}

func writeVersion(w http.ResponseWriter, r *http.Request, version skillVersionResponse) {
	if hasSkillsBeta(r) {
		httpapi.WriteJSON(w, http.StatusOK, version)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, stableVersion(version))
}

func writeVersionsPage(w http.ResponseWriter, r *http.Request, page pageResponse[skillVersionResponse]) {
	if hasSkillsBeta(r) {
		httpapi.WriteJSON(w, http.StatusOK, page)
		return
	}
	data := make([]stableVersionResponse, 0, len(page.Data))
	for _, version := range page.Data {
		data = append(data, stableVersion(version))
	}
	httpapi.WriteJSON(w, http.StatusOK, stablePageResponse[stableVersionResponse]{Data: data, NextPage: page.NextPage})
}

func skillDisplayLabel(r *http.Request, pkg skillPackage) (string, error) {
	field, limit := "display_name", 255
	if hasSkillsBeta(r) {
		field, limit = "display_title", 64
	}
	label := strings.TrimSpace(r.FormValue(field))
	if label == "" {
		label = pkg.Name
	}
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > limit || strings.ContainsAny(label, "\r\n") {
		return "", invalidSkillLabel(field, limit)
	}
	return label, nil
}

func mapSkillDeleteError(err error) error {
	if errors.Is(err, db.ErrSkillHasVersions) {
		return skillVersionsMustBeDeleted()
	}
	if errors.Is(err, db.ErrSkillLastVersion) {
		return skillLastVersionRequired()
	}
	return err
}
