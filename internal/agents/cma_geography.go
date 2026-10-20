package agents

import (
	"encoding/json"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func validateCMARosterGeography(modelRaw, multiRaw json.RawMessage, selfID string, selfVersion int, lookup func(agentReference) (json.RawMessage, error)) error {
	if len(multiRaw) == 0 {
		return nil
	}
	var multi cmaMultiagent
	if err := json.Unmarshal(multiRaw, &multi); err != nil {
		return err
	}
	if multi.Type != "multiagent_20261001" {
		return nil
	}
	parent, err := normalizeModel(modelRaw)
	if err != nil {
		return err
	}
	for _, setting := range []cmaDelegation{multi.Subagents, multi.Workflows} {
		if setting.Type != "enabled" {
			continue
		}
		for _, ref := range setting.PredefinedAgents {
			if ref.ID == selfID && ref.Version == selfVersion {
				continue
			}
			raw, err := lookup(ref)
			if err != nil {
				return err
			}
			child, err := normalizeModel(raw)
			if err != nil {
				return err
			}
			if parent.InferenceGeo != child.InferenceGeo {
				return errRosterInferenceGeoMismatch
			}
		}
	}
	return nil
}

func (h *Handler) validateCMARosterGeography(r *http.Request, principal auth.Principal, state agentState, selfID string, version int) error {
	return validateCMARosterGeography(state.Model, state.Multiagent, selfID, version, func(ref agentReference) (json.RawMessage, error) {
		record, err := h.db.GetAgentVersion(r.Context(), principal.WorkspaceUUID, ref.ID, ref.Version)
		if err != nil {
			return nil, errRosterAgentVersionNotFound
		}
		return record.Model, nil
	})
}
