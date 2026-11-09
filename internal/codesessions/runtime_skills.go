package codesessions

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type SessionSkillReader interface {
	ReadSessionSkill(context.Context, string, string, string) ([]byte, error)
}

type runtimeSkillDocument struct {
	runtimeSkillConfig
	Document string `json:"document"`
}

func (s *Service) WithSessionSkills(reader SessionSkillReader) *Service {
	s.skillReader = reader
	return s
}

func (h *Handler) handleSessionSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "code_session_id")
	if _, ok := h.sessionIngressAccessClaims(w, r, id, false); !ok {
		return
	}
	record, err := h.requireCodeSession(r.Context(), id)
	if err != nil {
		h.writeIngressLoadError(w, r, err)
		return
	}
	threadID := strings.TrimSpace(r.Header.Get(runtimeThreadHeader))
	snapshot, err := h.runtimeSkillSnapshot(r.Context(), record, threadID)
	if err != nil {
		h.errorAdapter.Write(w, r, runtimeSkillsFailure(err))
		return
	}
	documents, err := h.service.sessionSkillDocuments(r.Context(), record, snapshot, r.URL.Query().Get("directory"), threadID)
	if err != nil {
		h.errorAdapter.Write(w, r, runtimeSkillsFailure(err))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Skills []runtimeSkillDocument `json:"skills"`
	}{documents})
}

func (h *Handler) runtimeSkillSnapshot(ctx context.Context, record db.CodeSession, threadID string) (json.RawMessage, error) {
	if threadID != "" {
		return h.loadMCPThreadSnapshot(ctx, record, threadID)
	}
	session, found, err := h.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	return session.AgentSnapshot, nil
}

func (s *Service) sessionSkillDocuments(ctx context.Context, record db.CodeSession, snapshot json.RawMessage, directory, threadID string) ([]runtimeSkillDocument, error) {
	var config struct {
		RuntimeSkills []runtimeSkillConfig   `json:"runtime_skills"`
		Directories   map[string]string      `json:"skill_directories"`
		Subagents     *RuntimeSubagentConfig `json:"subagents"`
	}
	var metadata struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(record.Metadata, &metadata); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metadata.Config, &config); err != nil {
		return nil, err
	}
	selected, err := selectRuntimeSkills(config.RuntimeSkills, config.Directories, snapshot, threadID == "" && config.Subagents == nil)
	if err != nil {
		return nil, err
	}
	documents := make([]runtimeSkillDocument, 0, len(selected))
	for _, skill := range selected {
		skillDirectory := path.Base(path.Dir(skill.Path))
		if directory != "" && directory != skillDirectory {
			continue
		}
		if s.skillReader == nil {
			return nil, errRuntimeSkillsUnavailable
		}
		data, err := s.skillReader.ReadSessionSkill(ctx, record.WorkspaceUUID, record.SessionExternalID, path.Base(path.Dir(skill.Path)))
		if err != nil {
			return nil, err
		}
		document := string(data)
		if directory == "" {
			document, err = skillFrontmatter(document)
			if err != nil {
				return nil, err
			}
		}
		documents = append(documents, runtimeSkillDocument{skill, document})
	}
	if directory != "" && len(documents) == 0 {
		return nil, db.ErrNotFound
	}
	return documents, nil
}

func selectRuntimeSkills(skills []runtimeSkillConfig, directories map[string]string, snapshot json.RawMessage, allowLegacyRoot bool) ([]runtimeSkillConfig, error) {
	var agent struct {
		Skills []struct {
			Type    string `json:"type"`
			SkillID string `json:"skill_id"`
			Version string `json:"version"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(snapshot, &agent); err != nil {
		return nil, err
	}
	if allowLegacyRoot && directories == nil && len(agent.Skills) > 0 && len(agent.Skills) == len(skills) {
		return skills, nil
	}
	allowed := make(map[string]bool, len(agent.Skills))
	for _, ref := range agent.Skills {
		version := ref.Version
		if version == "" {
			version = "latest"
		}
		directory := directories[ref.Type+":"+ref.SkillID+":"+version]
		if directory == "" {
			return nil, errRuntimeSkillDirectory
		}
		allowed[directory] = true
	}
	selected := make([]runtimeSkillConfig, 0, len(allowed))
	for _, skill := range skills {
		if allowed[path.Base(path.Dir(skill.Path))] {
			selected = append(selected, skill)
		}
	}
	if len(selected) != len(allowed) {
		return nil, errRuntimeSkillDirectory
	}
	return selected, nil
}

func skillFrontmatter(document string) (string, error) {
	text := strings.ReplaceAll(document, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", nil
	}
	lines := strings.SplitAfter(text[4:], "\n")
	end := 4
	for _, line := range lines {
		end += len(line)
		if strings.TrimSuffix(line, "\n") == "---" {
			return text[:end], nil
		}
	}
	return "", errRuntimeSkillFrontmatter
}
