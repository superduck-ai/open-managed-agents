package vaults

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

func (h *Handler) resolveExternalVaultRuntime(
	ctx context.Context,
	principal auth.Principal,
) (agentruntime.Runtime, agentruntime.VaultRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, vaultRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	vaultRT, ok := rt.(agentruntime.VaultRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, vaultRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, vaultRT, scope, nil
}

func (h *Handler) createVaultExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	request, err := httpapi.DecodeObjectBodyAs[createVaultRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	displayName, err := requireNonEmptyString(request.DisplayName, "display_name")
	if err == nil {
		err = validateDisplayName(displayName)
	}
	if err != nil {
		return invalidRequest(err)
	}
	metadataRaw, err := normalizeVaultCreateMetadata(request.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return invalidRequest(err)
	}
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := vaultRT.CreateVault(r.Context(), scope, agentruntime.CreateVaultInput{
		DisplayName: displayName,
		Metadata:    metadata,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalVault(created))
	return nil
}

func (h *Handler) listVaultsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := parseLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodeExternalVaultOffset(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := vaultRT.ListVaults(r.Context(), scope, agentruntime.ListVaultsQuery{
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	data := make([]vaultResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromExternalVault(item))
	}
	var nextPage *string
	if page.HasMore {
		value := encodeExternalVaultOffset(offset + len(data))
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, vaultPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveVaultExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID string) error {
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	vault, err := vaultRT.GetVault(r.Context(), scope, vaultID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalVault(vault))
	return nil
}

func (h *Handler) updateVaultExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID string) error {
	request, err := httpapi.DecodeObjectBodyAs[updateVaultRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	var input agentruntime.UpdateVaultInput
	if len(request.DisplayName) > 0 {
		var name string
		if err := json.Unmarshal(request.DisplayName, &name); err != nil {
			return invalidRequest(errors.New("display_name must be a string"))
		}
		name = strings.TrimSpace(name)
		input.DisplayName = &name
	}
	if len(request.Metadata) > 0 {
		var metadata map[string]string
		if err := json.Unmarshal(request.Metadata, &metadata); err != nil {
			return invalidRequest(errors.New("metadata must be an object with string values"))
		}
		input.Metadata = metadata
		input.MetadataSet = true
	}
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	updated, err := vaultRT.UpdateVault(r.Context(), scope, vaultID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalVault(updated))
	return nil
}

func (h *Handler) archiveVaultExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID string) error {
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	archived, err := vaultRT.ArchiveVault(r.Context(), scope, vaultID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalVault(archived))
	return nil
}

func (h *Handler) deleteVaultExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID string) error {
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := vaultRT.DeleteVault(r.Context(), scope, vaultID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: vaultID, Type: "vault_deleted"})
	return nil
}

func (h *Handler) createCredentialExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID string) error {
	request, err := httpapi.DecodeObjectBodyAs[createCredentialRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if err := validateDisplayName(displayName); err != nil {
		return invalidRequest(err)
	}
	metadataRaw, err := normalizeMetadata(request.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return invalidRequest(err)
	}
	if len(request.Auth) == 0 || httpapi.IsJSONNull(request.Auth) {
		return invalidRequest(errors.New("auth is required"))
	}
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := vaultRT.CreateVaultCredential(r.Context(), scope, vaultID, agentruntime.CreateVaultCredentialInput{
		DisplayName: displayName,
		Metadata:    metadata,
		Auth:        request.Auth,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalCredential(created))
	return nil
}

func (h *Handler) listCredentialsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID string) error {
	limit, err := parseLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodeExternalVaultOffset(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := vaultRT.ListVaultCredentials(r.Context(), scope, vaultID, agentruntime.ListVaultCredentialsQuery{
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	data := make([]credentialResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromExternalCredential(item))
	}
	var nextPage *string
	if page.HasMore {
		value := encodeExternalVaultOffset(offset + len(data))
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, credentialPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveCredentialExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID, credentialID string) error {
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	cred, err := vaultRT.GetVaultCredential(r.Context(), scope, vaultID, credentialID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalCredential(cred))
	return nil
}

func (h *Handler) updateCredentialExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID, credentialID string) error {
	request, err := httpapi.DecodeObjectBodyAs[updateCredentialRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	var input agentruntime.UpdateVaultCredentialInput
	if len(request.DisplayName) > 0 && !httpapi.IsJSONNull(request.DisplayName) {
		var name string
		if err := json.Unmarshal(request.DisplayName, &name); err != nil {
			return invalidRequest(errors.New("display_name must be a string"))
		}
		name = strings.TrimSpace(name)
		input.DisplayName = &name
	}
	if len(request.Auth) > 0 && !httpapi.IsJSONNull(request.Auth) {
		input.Auth = request.Auth
	}
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if len(request.Metadata) > 0 {
		current, err := vaultRT.GetVaultCredential(r.Context(), scope, vaultID, credentialID)
		if err != nil {
			return err
		}
		currentRaw, _ := json.Marshal(current.Metadata)
		patchedRaw, err := patchMetadata(currentRaw, request.Metadata)
		if err != nil {
			return invalidRequest(err)
		}
		var patched map[string]string
		if err := json.Unmarshal(patchedRaw, &patched); err != nil {
			return invalidRequest(err)
		}
		input.Metadata = patched
		input.MetadataSet = true
	}
	updated, err := vaultRT.UpdateVaultCredential(r.Context(), scope, vaultID, credentialID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalCredential(updated))
	return nil
}

func (h *Handler) archiveCredentialExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID, credentialID string) error {
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	archived, err := vaultRT.ArchiveVaultCredential(r.Context(), scope, vaultID, credentialID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalCredential(archived))
	return nil
}

func (h *Handler) deleteCredentialExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, vaultID, credentialID string) error {
	rt, vaultRT, scope, err := h.resolveExternalVaultRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := vaultRT.DeleteVaultCredential(r.Context(), scope, vaultID, credentialID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: credentialID, Type: "vault_credential_deleted"})
	return nil
}

func encodeExternalVaultOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(offset)))
}

func decodeExternalVaultOffset(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return 0, errors.New("page cursor is invalid")
	}
	value, ok := strings.CutPrefix(string(decoded), "offset:")
	if !ok {
		return 0, errors.New("page cursor is invalid")
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, errors.New("page cursor is invalid")
	}
	return n, nil
}

func responseFromExternalVault(item agentruntime.Vault) vaultResponse {
	metadataRaw := json.RawMessage(`{}`)
	if len(item.Metadata) > 0 {
		if encoded, err := json.Marshal(item.Metadata); err == nil {
			metadataRaw = encoded
		}
	}
	return vaultResponse{
		ID:          item.ID,
		ArchivedAt:  httpapi.OptionalTime(item.ArchivedAt),
		CreatedAt:   httpapi.FormatTime(item.CreatedAt),
		DisplayName: item.DisplayName,
		Metadata:    metadataRaw,
		Type:        "vault",
		UpdatedAt:   httpapi.FormatTime(item.UpdatedAt),
	}
}

func responseFromExternalCredential(item agentruntime.VaultCredential) credentialResponse {
	metadataRaw := json.RawMessage(`{}`)
	if len(item.Metadata) > 0 {
		if encoded, err := json.Marshal(item.Metadata); err == nil {
			metadataRaw = encoded
		}
	}
	var authPayload credentialAuth
	if len(item.Auth) > 0 {
		_ = json.Unmarshal(item.Auth, &authPayload)
	}
	return credentialResponse{
		ID:          item.ID,
		ArchivedAt:  httpapi.OptionalTime(item.ArchivedAt),
		Auth:        authPayload,
		CreatedAt:   httpapi.FormatTime(item.CreatedAt),
		Metadata:    metadataRaw,
		Type:        "vault_credential",
		UpdatedAt:   httpapi.FormatTime(item.UpdatedAt),
		VaultID:     item.VaultID,
		DisplayName: item.DisplayName,
	}
}
