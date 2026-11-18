package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type channelGitMount struct {
	URL       string          `json:"url"`
	MountPath string          `json:"mount_path,omitempty"`
	Checkout  json.RawMessage `json:"checkout,omitempty"`
	Token     string          `json:"authorization_token,omitempty"`
}

type channelMounts struct {
	LegacyResourceFingerprint string                 `json:"legacy_resource_fingerprint,omitempty"`
	Resources                 []channelResourceMount `json:"resources,omitempty"`
	AgentVersion              int                    `json:"agent_version,omitzero"`
	MemoryStoreID             string                 `json:"memory_store_id,omitempty"`
	VaultID                   string                 `json:"vault_id,omitempty"`
	VaultIDs                  []string               `json:"vault_ids,omitempty"`
	GitRepository             *channelGitMount       `json:"git_repository,omitempty"`
}

func parseChannelMounts(raw string) channelMounts {
	mounts := channelMounts{VaultIDs: []string{}}
	if strings.TrimSpace(raw) == "" {
		return mounts
	}
	_ = json.Unmarshal([]byte(raw), &mounts)
	if len(mounts.VaultIDs) == 0 && mounts.VaultID != "" {
		mounts.VaultIDs = []string{mounts.VaultID}
	}
	if mounts.VaultIDs == nil {
		mounts.VaultIDs = []string{}
	}
	return mounts
}

func (m channelMounts) encode() string {
	m.GitRepository = gitMountWithoutToken(m.GitRepository)
	encoded, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func gitMountWithoutToken(mount *channelGitMount) *channelGitMount {
	if mount == nil {
		return nil
	}
	clone := *mount
	clone.Token = ""
	return &clone
}

func (s *localService) applyChannelMounts(ctx context.Context, principal auth.Principal, record *db.ChannelRecord, input channelMutation) error {
	mounts := parseChannelMounts(record.Mounts)
	version, err := s.channelAgentVersion(ctx, principal, record.AgentExternalID, mounts.AgentVersion, input.AgentVersion)
	if err != nil {
		return err
	}
	mounts.AgentVersion = version
	value, present, err := rawStringValue(input.MemoryStoreID)
	if err != nil {
		return invalidChannelMounts(err)
	}
	if present {
		mounts.MemoryStoreID = value
	}
	mounts.VaultIDs, err = s.channelVaultIDs(ctx, principal, mounts.VaultIDs, input)
	if err != nil {
		return err
	}
	mounts.VaultID = ""
	if mounts.MemoryStoreID != "" {
		store, err := s.store.GetMemoryStore(ctx, principal.WorkspaceUUID, mounts.MemoryStoreID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && (store.ArchivedAt != nil || store.DeletedAt != nil)) {
			return invalidMemoryMount(err)
		}
		if err != nil {
			return unavailable(err)
		}
	}
	if err := s.applyChannelResources(ctx, principal, record, &mounts, input); err != nil {
		return err
	}
	record.Mounts = mounts.encode()
	return nil
}

func channelMountView(record db.ChannelRecord) map[string]any {
	mounts := parseChannelMounts(record.Mounts)
	view := map[string]any{
		"agent_version":   nil,
		"memory_store_id": nil,
		"vault_id":        nil,
		"vault_ids":       mounts.VaultIDs,
		"git_repository":  nil,
	}
	if mounts.AgentVersion > 0 {
		view["agent_version"] = mounts.AgentVersion
	}
	if mounts.MemoryStoreID != "" {
		view["memory_store_id"] = mounts.MemoryStoreID
	}
	if len(mounts.VaultIDs) == 1 {
		view["vault_id"] = mounts.VaultIDs[0]
	}
	if mounts.GitRepository != nil {
		git := map[string]any{"url": mounts.GitRepository.URL}
		if mounts.GitRepository.MountPath != "" {
			git["mount_path"] = mounts.GitRepository.MountPath
		}
		if len(mounts.GitRepository.Checkout) > 0 {
			git["checkout"] = json.RawMessage(mounts.GitRepository.Checkout)
		}
		git["tokenConfigured"] = record.GitTokenEnvelope != nil
		view["git_repository"] = git
	}
	resources := []map[string]any{}
	if mounts.Resources != nil {
		gitCount := 0
		for _, resource := range mounts.Resources {
			value := resource.payload()
			if resource.Git != nil {
				value["tokenConfigured"] = resource.TokenEnvelope != nil
				gitCount++
				alias := maps.Clone(value)
				delete(alias, "type")
				view["git_repository"] = alias
			}
			resources = append(resources, value)
		}
		if gitCount != 1 {
			view["git_repository"] = nil
		}
	} else if git, ok := view["git_repository"].(map[string]any); ok {
		resource := maps.Clone(git)
		resource["type"] = sessionresource.GitRepositoryType
		resources = append(resources, resource)
	}
	view["resources"] = resources
	return view
}

func normalizeChannelGitMount(mount channelGitMount) (sessionresource.GitRepositorySpec, error) {
	urlRaw, err := json.Marshal(mount.URL)
	if err != nil {
		return sessionresource.GitRepositorySpec{}, err
	}
	var mountRaw []byte
	if mount.MountPath != "" {
		mountRaw, err = json.Marshal(mount.MountPath)
		if err != nil {
			return sessionresource.GitRepositorySpec{}, err
		}
	}
	checkout := mount.Checkout
	if len(bytes.TrimSpace(checkout)) == 0 {
		checkout = json.RawMessage("null")
	}
	return sessionresource.NormalizeGitRepositorySpec(urlRaw, mountRaw, checkout)
}

func rawStringValue(raw json.RawMessage) (string, bool, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", false, nil
	}
	if trimmed == "null" {
		return "", true, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, err
	}
	return strings.TrimSpace(value), true, nil
}
