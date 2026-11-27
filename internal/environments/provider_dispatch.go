package environments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func (h *Handler) resolveExternalEnvironmentRuntime(ctx context.Context, principal auth.Principal) (agentruntime.Runtime, agentruntime.EnvironmentRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, environmentRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	envRT, ok := rt.(agentruntime.EnvironmentRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, environmentRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, envRT, scope, nil
}

func (h *Handler) createExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	body, err := httpapi.DecodeObjectBodyAs[environmentMutationRequest](w, r, maxEnvironmentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	name, err := parseRequiredRawString(body.Name, "name")
	if err != nil {
		return invalidRequest(err)
	}
	description, err := parseOptionalDescription(body.Description)
	if err != nil {
		return invalidRequest(err)
	}
	metadata, err := decodeEnvironmentMetadataMap(body.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	configRaw := rawOrDefault(body.Config, `{}`)
	if isJSONNull(configRaw) {
		configRaw = json.RawMessage(`{}`)
	}
	rt, envRT, scope, err := h.resolveExternalEnvironmentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := envRT.CreateEnvironment(r.Context(), scope, agentruntime.CreateEnvironmentInput{
		Name:        name,
		Description: description,
		Config:      configRaw,
		Metadata:    metadata,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeEnvironment(created))
	return nil
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := parseLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseExternalEnvironmentOffset(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", "all":
	case "active":
		includeArchived = false
	case "archived":
		includeArchived = true
	default:
		return invalidEnvironmentListStatus()
	}
	rt, envRT, scope, err := h.resolveExternalEnvironmentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := envRT.ListEnvironments(r.Context(), scope, agentruntime.ListEnvironmentsQuery{
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	data := make([]environmentResponse, 0, len(page.Items))
	for _, item := range page.Items {
		if status == "archived" && item.ArchivedAt == nil {
			continue
		}
		data = append(data, responseFromRuntimeEnvironment(item))
	}
	var nextPage *string
	if page.HasMore {
		cursor := page.NextCursor
		if cursor == "" {
			cursor = strconv.Itoa(offset + len(page.Items))
		}
		nextPage = &cursor
	}
	httpapi.WriteJSON(w, http.StatusOK, environmentPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, environmentID string) error {
	rt, envRT, scope, err := h.resolveExternalEnvironmentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	env, err := envRT.GetEnvironment(r.Context(), scope, environmentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeEnvironment(env))
	return nil
}

func (h *Handler) updateExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, environmentID string) error {
	body, err := httpapi.DecodeObjectBodyAs[environmentMutationRequest](w, r, maxEnvironmentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	input, err := buildExternalUpdateEnvironmentInput(body)
	if err != nil {
		return invalidRequest(err)
	}
	rt, envRT, scope, err := h.resolveExternalEnvironmentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	updated, err := envRT.UpdateEnvironment(r.Context(), scope, environmentID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeEnvironment(updated))
	return nil
}

func (h *Handler) archiveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, environmentID string) error {
	rt, envRT, scope, err := h.resolveExternalEnvironmentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	archived, err := envRT.ArchiveEnvironment(r.Context(), scope, environmentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeEnvironment(archived))
	return nil
}

func (h *Handler) deleteExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, environmentID string) error {
	rt, envRT, scope, err := h.resolveExternalEnvironmentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := envRT.DeleteEnvironment(r.Context(), scope, environmentID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: environmentID, Type: "environment_deleted"})
	return nil
}

func buildExternalUpdateEnvironmentInput(body *environmentMutationRequest) (agentruntime.UpdateEnvironmentInput, error) {
	var input agentruntime.UpdateEnvironmentInput
	if len(body.Name) > 0 {
		name, err := parseRequiredRawString(body.Name, "name")
		if err != nil {
			return agentruntime.UpdateEnvironmentInput{}, err
		}
		input.Name = &name
	}
	if len(body.Description) > 0 {
		description, err := parseOptionalDescription(body.Description)
		if err != nil {
			return agentruntime.UpdateEnvironmentInput{}, err
		}
		input.Description = &description
	}
	if len(body.Config) > 0 && !isJSONNull(body.Config) {
		input.Config = body.Config
	}
	if len(body.Metadata) > 0 {
		metadata, err := decodeEnvironmentMetadataMap(body.Metadata)
		if err != nil {
			return agentruntime.UpdateEnvironmentInput{}, err
		}
		input.Metadata = metadata
	}
	return input, nil
}

func decodeEnvironmentMetadataMap(raw json.RawMessage) (map[string]string, error) {
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

func parseExternalEnvironmentOffset(raw string) (int, error) {
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

func responseFromRuntimeEnvironment(env agentruntime.Environment) environmentResponse {
	state := "active"
	if env.ArchivedAt != nil {
		state = "archived"
	}
	metadata := json.RawMessage(`{}`)
	if len(env.Metadata) > 0 {
		if encoded, err := json.Marshal(env.Metadata); err == nil {
			metadata = encoded
		}
	}
	return environmentResponse{
		ID:          env.ID,
		ArchivedAt:  optionalTime(env.ArchivedAt),
		Config:      httpapi.RawOr(env.Config, `{}`),
		CreatedAt:   formatTime(env.CreatedAt),
		Description: env.Description,
		Metadata:    metadata,
		Name:        env.Name,
		Scope:       "organization",
		State:       state,
		Type:        "environment",
		UpdatedAt:   formatTime(env.UpdatedAt),
	}
}
