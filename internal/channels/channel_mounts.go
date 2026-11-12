package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
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
	MemoryStoreID string           `json:"memory_store_id,omitempty"`
	VaultID       string           `json:"vault_id,omitempty"`
	GitRepository *channelGitMount `json:"git_repository,omitempty"`
}

func parseChannelMounts(raw string) channelMounts {
	var mounts channelMounts
	if strings.TrimSpace(raw) == "" {
		return mounts
	}
	_ = json.Unmarshal([]byte(raw), &mounts)
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
	if value, present := rawStringValue(input.MemoryStoreID); present {
		mounts.MemoryStoreID = value
	}
	if value, present := rawStringValue(input.VaultID); present {
		mounts.VaultID = value
	}
	if input.GitRepository != nil {
		trimmed := strings.TrimSpace(string(input.GitRepository))
		if trimmed == "" || trimmed == "null" {
			mounts.GitRepository = nil
			record.GitTokenEnvelope = nil
		} else {
			var mount channelGitMount
			if err := json.Unmarshal(input.GitRepository, &mount); err != nil {
				return invalidChannelMounts(err)
			}
			mount.URL = strings.TrimSpace(mount.URL)
			if err := validateGitMount(mount); err != nil {
				return invalidChannelMounts(err)
			}
			token := strings.TrimSpace(mount.Token)
			if token == "" {
				previous := mounts.GitRepository
				if record.GitTokenEnvelope == nil || previous == nil || previous.URL != mount.URL {
					record.GitTokenEnvelope = nil
				}
			} else {
				envelope, err := sealChannelSecret(ctx, s.cipher, *record, token)
				if err != nil {
					return unavailable(err)
				}
				record.GitTokenEnvelope = envelope
			}
			mounts.GitRepository = &mount
		}
	}
	if mounts.MemoryStoreID != "" {
		store, err := s.store.GetMemoryStore(ctx, principal.WorkspaceUUID, mounts.MemoryStoreID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && (store.ArchivedAt != nil || store.DeletedAt != nil)) {
			return invalidMemoryMount(err)
		}
		if err != nil {
			return unavailable(err)
		}
	}
	if mounts.VaultID != "" {
		vault, err := s.store.GetVault(ctx, principal.WorkspaceUUID, mounts.VaultID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && (vault.ArchivedAt != nil || vault.DeletedAt != nil)) {
			return invalidVaultMount(err)
		}
		if err != nil {
			return unavailable(err)
		}
	}
	record.Mounts = mounts.encode()
	return nil
}

func channelMountView(record db.ChannelRecord) map[string]any {
	mounts := parseChannelMounts(record.Mounts)
	view := map[string]any{
		"memory_store_id": nil,
		"vault_id":        nil,
		"git_repository":  nil,
	}
	if mounts.MemoryStoreID != "" {
		view["memory_store_id"] = mounts.MemoryStoreID
	}
	if mounts.VaultID != "" {
		view["vault_id"] = mounts.VaultID
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
	return view
}

func validateGitMount(mount channelGitMount) error {
	urlRaw, err := json.Marshal(mount.URL)
	if err != nil {
		return err
	}
	mountRaw, err := json.Marshal(mount.MountPath)
	if err != nil {
		return err
	}
	checkout := mount.Checkout
	if len(bytes.TrimSpace(checkout)) == 0 {
		checkout = json.RawMessage("null")
	}
	if _, err := sessionresource.NormalizeGitRepositorySpec(urlRaw, mountRaw, checkout); err != nil {
		return fmt.Errorf("git_repository 配置无效: %w", err)
	}
	return nil
}

func rawStringValue(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", false
	}
	if trimmed == "null" {
		return "", true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func invalidChannelMounts(err error) error {
	return apperr.New(apperr.InvalidArgument, "渠道挂载配置无效", err)
}
