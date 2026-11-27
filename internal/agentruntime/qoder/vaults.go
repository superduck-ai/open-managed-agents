package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const vaultHydrateConcurrency = 5

type qoderVault struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata"`
	ArchivedAt  *time.Time        `json:"archived_at"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type qoderCreateVaultRequest struct {
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type qoderVaultCredential struct {
	ID          string            `json:"id"`
	VaultID     string            `json:"vault_id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata"`
	Auth        json.RawMessage   `json:"auth"`
	ArchivedAt  *time.Time        `json:"archived_at"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type qoderVaultCredentialsEnvelope struct {
	Data        []qoderVaultCredential `json:"data"`
	Credentials []qoderVaultCredential `json:"credentials"`
	NextPage    *string                `json:"next_page"`
}

type vaultBindingSummary struct {
	DisplayName string `json:"display_name,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
}

func (r *Runtime) CreateVault(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateVaultInput) (agentruntime.Vault, error) {
	reqBody := qoderCreateVaultRequest{
		DisplayName: input.DisplayName,
		Metadata:    injectWatermark(input.Metadata, scope),
	}
	var remote qoderVault
	if err := r.doJSON(ctx, http.MethodPost, "/vaults", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Vault{}, err
	}
	omaID, err := ids.New("vlt_")
	if err != nil {
		return agentruntime.Vault{}, agentruntime.InvalidArgumentError("failed to generate vault id", err)
	}
	mapped := mapQoderVault(omaID, remote)
	summary, _ := json.Marshal(vaultBindingSummary{
		DisplayName: mapped.DisplayName,
		Archived:    mapped.ArchivedAt != nil,
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeVault,
		OMAExternalID: omaID,
		RemoteID:      remote.ID,
	}, summary); err != nil {
		return agentruntime.Vault{}, err
	}
	return mapped, nil
}

func (r *Runtime) GetVault(ctx context.Context, scope agentruntime.Scope, vaultID string) (agentruntime.Vault, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.Vault{}, err
	}
	var remote qoderVault
	if err := r.doJSON(ctx, http.MethodGet, "/vaults/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.Vault{}, err
	}
	mapped := mapQoderVault(binding.OMAExternalID, remote)
	r.refreshVaultBinding(ctx, scope, binding, mapped)
	return mapped, nil
}

func (r *Runtime) ListVaults(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListVaultsQuery) (agentruntime.Page[agentruntime.Vault], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeVault, "", "", query.IncludeArchived, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.Vault]{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := r.hydrateVaults(ctx, rows)
	page := agentruntime.Page[agentruntime.Vault]{
		Items:   items,
		HasMore: hasMore,
	}
	if len(items) > 0 {
		page.FirstID = items[0].ID
		page.LastID = items[len(items)-1].ID
	}
	return page, nil
}

func (r *Runtime) UpdateVault(ctx context.Context, scope agentruntime.Scope, vaultID string, _ agentruntime.UpdateVaultInput) (agentruntime.Vault, error) {
	if _, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID); err != nil {
		return agentruntime.Vault{}, err
	}
	return agentruntime.Vault{}, apperr.New(apperr.NotImplemented, "Updating vaults is not available on qoder runtime", nil)
}

func (r *Runtime) ArchiveVault(ctx context.Context, scope agentruntime.Scope, vaultID string) (agentruntime.Vault, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.Vault{}, err
	}
	var remote qoderVault
	if err := r.doJSON(ctx, http.MethodPost, "/vaults/"+url.PathEscape(binding.RemoteID)+"/archive", nil, nil, nil, &remote); err != nil {
		return agentruntime.Vault{}, err
	}
	mapped := mapQoderVault(binding.OMAExternalID, remote)
	summary, _ := json.Marshal(vaultBindingSummary{
		DisplayName: mapped.DisplayName,
		Archived:    true,
	})
	_, _ = r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeVault, binding.OMAExternalID, "archived", summary)
	return mapped, nil
}

func (r *Runtime) DeleteVault(ctx context.Context, scope agentruntime.Scope, vaultID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return err
	}
	if err := r.doJSON(ctx, http.MethodDelete, "/vaults/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil); err != nil {
		return err
	}
	return r.bindings.SoftDelete(ctx, scope, agentruntime.ResourceTypeVault, binding.OMAExternalID)
}

func (r *Runtime) CreateVaultCredential(ctx context.Context, scope agentruntime.Scope, vaultID string, input agentruntime.CreateVaultCredentialInput) (agentruntime.VaultCredential, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.VaultCredential{}, err
	}
	payload := map[string]any{
		"display_name": input.DisplayName,
		"auth":         input.Auth,
	}
	if len(input.Metadata) > 0 {
		payload["metadata"] = input.Metadata
	}
	var remote qoderVaultCredential
	if err := r.doJSON(ctx, http.MethodPost, "/vaults/"+url.PathEscape(binding.RemoteID)+"/credentials", nil, nil, payload, &remote); err != nil {
		return agentruntime.VaultCredential{}, err
	}
	return mapQoderVaultCredential(binding.OMAExternalID, remote), nil
}

func (r *Runtime) GetVaultCredential(ctx context.Context, scope agentruntime.Scope, vaultID string, credentialID string) (agentruntime.VaultCredential, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.VaultCredential{}, err
	}
	var remote qoderVaultCredential
	path := "/vaults/" + url.PathEscape(binding.RemoteID) + "/credentials/" + url.PathEscape(credentialID)
	if err := r.doJSON(ctx, http.MethodGet, path, nil, nil, nil, &remote); err != nil {
		return agentruntime.VaultCredential{}, err
	}
	return mapQoderVaultCredential(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ListVaultCredentials(ctx context.Context, scope agentruntime.Scope, vaultID string, query agentruntime.ListVaultCredentialsQuery) (agentruntime.Page[agentruntime.VaultCredential], error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.Page[agentruntime.VaultCredential]{}, err
	}
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	collected := make([]agentruntime.VaultCredential, 0)
	pageToken := ""
	for pageCount := 0; pageCount < 20; pageCount++ {
		params := url.Values{}
		params.Set("limit", "100")
		if query.IncludeArchived {
			params.Set("include_archived", strconv.FormatBool(true))
		}
		if pageToken != "" {
			params.Set("page", pageToken)
		}
		var env qoderVaultCredentialsEnvelope
		path := "/vaults/" + url.PathEscape(binding.RemoteID) + "/credentials"
		if err := r.doJSON(ctx, http.MethodGet, path, params, nil, nil, &env); err != nil {
			return agentruntime.Page[agentruntime.VaultCredential]{}, err
		}
		raw := env.Data
		if len(raw) == 0 {
			raw = env.Credentials
		}
		for _, item := range raw {
			if !query.IncludeArchived && item.ArchivedAt != nil {
				continue
			}
			collected = append(collected, mapQoderVaultCredential(binding.OMAExternalID, item))
		}
		if env.NextPage == nil || *env.NextPage == "" {
			break
		}
		pageToken = *env.NextPage
	}
	if offset >= len(collected) {
		return agentruntime.Page[agentruntime.VaultCredential]{Items: []agentruntime.VaultCredential{}}, nil
	}
	end := offset + limit
	hasMore := end < len(collected)
	if end > len(collected) {
		end = len(collected)
	}
	slice := collected[offset:end]
	page := agentruntime.Page[agentruntime.VaultCredential]{
		Items:   slice,
		HasMore: hasMore,
	}
	if len(slice) > 0 {
		page.FirstID = slice[0].ID
		page.LastID = slice[len(slice)-1].ID
	}
	return page, nil
}

func (r *Runtime) UpdateVaultCredential(ctx context.Context, scope agentruntime.Scope, vaultID string, credentialID string, input agentruntime.UpdateVaultCredentialInput) (agentruntime.VaultCredential, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.VaultCredential{}, err
	}
	payload := make(map[string]any)
	if input.MetadataSet {
		payload["metadata"] = input.Metadata
	}
	if len(bytes.TrimSpace(input.Auth)) > 0 {
		payload["auth"] = input.Auth
	}
	var remote qoderVaultCredential
	path := "/vaults/" + url.PathEscape(binding.RemoteID) + "/credentials/" + url.PathEscape(credentialID)
	if err := r.doJSON(ctx, http.MethodPost, path, nil, nil, payload, &remote); err != nil {
		return agentruntime.VaultCredential{}, err
	}
	return mapQoderVaultCredential(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ArchiveVaultCredential(ctx context.Context, scope agentruntime.Scope, vaultID string, credentialID string) (agentruntime.VaultCredential, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return agentruntime.VaultCredential{}, err
	}
	var remote qoderVaultCredential
	path := "/vaults/" + url.PathEscape(binding.RemoteID) + "/credentials/" + url.PathEscape(credentialID) + "/archive"
	if err := r.doJSON(ctx, http.MethodPost, path, nil, nil, nil, &remote); err != nil {
		return agentruntime.VaultCredential{}, err
	}
	return mapQoderVaultCredential(binding.OMAExternalID, remote), nil
}

func (r *Runtime) DeleteVaultCredential(ctx context.Context, scope agentruntime.Scope, vaultID string, credentialID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, vaultID)
	if err != nil {
		return err
	}
	path := "/vaults/" + url.PathEscape(binding.RemoteID) + "/credentials/" + url.PathEscape(credentialID)
	return r.doJSON(ctx, http.MethodDelete, path, nil, nil, nil, nil)
}

func (r *Runtime) mapRemoteVaultIDs(ctx context.Context, scope agentruntime.Scope, remoteVaultIDs []string) []string {
	if len(remoteVaultIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(remoteVaultIDs))
	for _, remoteID := range remoteVaultIDs {
		if binding, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeVault, remoteID); err == nil {
			out = append(out, binding.OMAExternalID)
		} else {
			out = append(out, remoteID)
		}
	}
	return out
}

func (r *Runtime) hydrateVaults(ctx context.Context, rows []db.RuntimeResourceBinding) []agentruntime.Vault {
	if len(rows) == 0 {
		return nil
	}
	items := make([]agentruntime.Vault, len(rows))
	sem := make(chan struct{}, vaultHydrateConcurrency)
	var wg sync.WaitGroup
	for idx, row := range rows {
		wg.Add(1)
		go func(i int, binding db.RuntimeResourceBinding) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var remote qoderVault
			if err := r.doJSON(ctx, http.MethodGet, "/vaults/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
				r.logger.WarnContext(ctx, "qoder vault hydrate failed, falling back to binding summary", "oma_id", binding.OMAExternalID, "error", err)
				items[i] = vaultFromBindingSummary(binding)
				return
			}
			items[i] = mapQoderVault(binding.OMAExternalID, remote)
		}(idx, row)
	}
	wg.Wait()
	return items
}

func (r *Runtime) refreshVaultBinding(ctx context.Context, scope agentruntime.Scope, binding db.RuntimeResourceBinding, mapped agentruntime.Vault) {
	state := "active"
	if mapped.ArchivedAt != nil {
		state = "archived"
	}
	summary, _ := json.Marshal(vaultBindingSummary{
		DisplayName: mapped.DisplayName,
		Archived:    mapped.ArchivedAt != nil,
	})
	_, _ = r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeVault, binding.OMAExternalID, state, summary)
}

func mapQoderVault(omaID string, remote qoderVault) agentruntime.Vault {
	return agentruntime.Vault{
		ID:          omaID,
		DisplayName: remote.DisplayName,
		Metadata:    stripWatermark(remote.Metadata),
		ArchivedAt:  remote.ArchivedAt,
		CreatedAt:   remote.CreatedAt,
		UpdatedAt:   remote.UpdatedAt,
	}
}

func vaultFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.Vault {
	var summary vaultBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	var archivedAt *time.Time
	if row.State == "archived" || summary.Archived {
		t := row.UpdatedAt
		archivedAt = &t
	}
	return agentruntime.Vault{
		ID:          row.OMAExternalID,
		DisplayName: summary.DisplayName,
		ArchivedAt:  archivedAt,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func mapQoderVaultCredential(omaVaultID string, remote qoderVaultCredential) agentruntime.VaultCredential {
	return agentruntime.VaultCredential{
		ID:          remote.ID,
		VaultID:     omaVaultID,
		DisplayName: remote.DisplayName,
		Metadata:    remote.Metadata,
		Auth:        remote.Auth,
		ArchivedAt:  remote.ArchivedAt,
		CreatedAt:   remote.CreatedAt,
		UpdatedAt:   remote.UpdatedAt,
	}
}
