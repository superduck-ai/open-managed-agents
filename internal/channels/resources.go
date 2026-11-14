package channels

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type channelResourceMount struct {
	Type          string                             `json:"type"`
	FileID        string                             `json:"file_id,omitempty"`
	MountPath     string                             `json:"mount_path,omitempty"`
	Git           *sessionresource.GitRepositorySpec `json:"git,omitempty"`
	TokenEnvelope *string                            `json:"token_envelope,omitempty"`
}

type channelResourceInput struct {
	Type               string         `json:"type"`
	FileID             jsontext.Value `json:"file_id"`
	Source             jsontext.Value `json:"source"`
	MountPath          jsontext.Value `json:"mount_path"`
	URL                jsontext.Value `json:"url"`
	Checkout           jsontext.Value `json:"checkout"`
	AuthorizationToken jsontext.Value `json:"authorization_token"`
}

type normalizedChannelResource struct {
	mount    channelResourceMount
	file     *sessionresource.FileSpec
	token    string
	tokenSet bool
}

type channelResourceBundle struct {
	Resources   []map[string]any
	VaultIDs    []string
	Fingerprint string
}

func configuredChannelResources(record db.ChannelRecord) ([]channelResourceMount, error) {
	mounts := parseChannelMounts(record.Mounts)
	if mounts.Resources != nil {
		return mounts.Resources, nil
	}
	resources := []channelResourceMount{}
	if mounts.GitRepository != nil {
		spec, err := normalizeChannelGitMount(*mounts.GitRepository)
		if err != nil {
			return nil, err
		}
		resources = append(resources, channelResourceMount{Type: sessionresource.GitRepositoryType, Git: &spec, TokenEnvelope: record.GitTokenEnvelope})
	}
	return resources, nil
}

func (s *localService) normalizeChannelResource(ctx context.Context, principal auth.Principal, input channelResourceInput) (normalizedChannelResource, error) {
	value := normalizedChannelResource{mount: channelResourceMount{Type: input.Type}}
	switch input.Type {
	case sessionresource.FileType:
		id, err := sessionresource.ParseFileID(input.FileID)
		if err != nil {
			return value, err
		}
		if _, err := s.store.GetFile(ctx, principal.WorkspaceUUID, id); err != nil {
			return value, err
		}
		spec, err := sessionresource.NormalizeFileSpec(id, input.Source, input.MountPath)
		if err != nil {
			return value, err
		}
		binding, err := spec.SessionFileBinding("")
		if err != nil {
			return value, err
		}
		value.mount.FileID, value.mount.MountPath = binding.FileID, binding.MountPath
		value.file = &spec
	case sessionresource.GitRepositoryType:
		spec, err := sessionresource.NormalizeGitRepositorySpec(input.URL, input.MountPath, input.Checkout)
		if err != nil {
			return value, err
		}
		token, err := sessionresource.ParseGitTokenInput(input.AuthorizationToken)
		if err != nil {
			return value, err
		}
		value.mount.Git, value.token = &spec, token
		value.tokenSet = len(input.AuthorizationToken) > 0 && string(input.AuthorizationToken) != `""`
	default:
		return value, errors.New("resources 仅支持file和github_repository，Memory请使用现有独立配置")
	}
	return value, nil
}

func legacyChannelResourceInput(raw []byte) ([]channelResourceInput, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var mount channelGitMount
	if err := json.Unmarshal(raw, &mount); err != nil {
		return nil, err
	}
	input := channelResourceInput{Type: sessionresource.GitRepositoryType, Checkout: jsontext.Value(mount.Checkout)}
	input.URL, _ = json.Marshal(strings.TrimSpace(mount.URL))
	if mount.MountPath != "" {
		input.MountPath, _ = json.Marshal(mount.MountPath)
	}
	if token := strings.TrimSpace(mount.Token); token != "" {
		input.AuthorizationToken, _ = json.Marshal(token)
	}
	return []channelResourceInput{input}, nil
}

func (s *localService) applyChannelResources(ctx context.Context, principal auth.Principal, record *db.ChannelRecord, mounts *channelMounts, input channelMutation) error {
	if input.Resources == nil && input.GitRepository == nil {
		if len(mounts.Resources) == sessioncontract.MaxResources && mounts.MemoryStoreID != "" {
			return invalidChannelMounts(fmt.Errorf("资源最多%d项", sessioncontract.MaxResources))
		}
		return nil
	}
	if input.Resources != nil && input.GitRepository != nil {
		return invalidChannelMounts(errors.New("resources与git_repository不能同时提交"))
	}
	current, err := configuredChannelResources(*record)
	if err != nil {
		return invalidChannelMounts(err)
	}
	var items []channelResourceInput
	if input.Resources != nil {
		if err := json.Unmarshal(input.Resources, &items); err != nil {
			return invalidChannelMounts(err)
		}
	} else {
		if len(current) > 1 || len(current) == 1 && current[0].Type != sessionresource.GitRepositoryType {
			return invalidChannelMounts(errors.New("当前有多个资源或文件，请使用resources更新"))
		}
		items, err = legacyChannelResourceInput(input.GitRepository)
		if err != nil {
			return invalidChannelMounts(err)
		}
	}
	count := len(items)
	if mounts.MemoryStoreID != "" {
		count++
	}
	if count > sessioncontract.MaxResources {
		return invalidChannelMounts(fmt.Errorf("资源最多%d项", sessioncontract.MaxResources))
	}
	values := make([]normalizedChannelResource, 0, len(items))
	files := []sessionresource.FileSpec{}
	gits := []sessionresource.GitRepositorySpec{}
	for _, item := range items {
		value, err := s.normalizeChannelResource(ctx, principal, item)
		if err != nil {
			return invalidChannelMounts(err)
		}
		values = append(values, value)
		if value.file != nil {
			files = append(files, *value.file)
		}
		if value.mount.Git != nil {
			gits = append(gits, *value.mount.Git)
		}
	}
	if err := errors.Join(sessionresource.ValidateFileSpecs(files), sessionresource.ValidateGitRepositoryConflicts(gits)); err != nil {
		return invalidChannelMounts(err)
	}
	resources := make([]channelResourceMount, 0, len(values))
	for _, value := range values {
		resource, err := s.sealChannelResource(ctx, *record, current, value)
		if err != nil {
			return unavailable(err)
		}
		resources = append(resources, resource)
	}

	if mounts.LegacyResourceFingerprint == "" && mounts.Resources == nil {
		fingerprint, err := channelResourceFingerprint(current)
		if err != nil {
			return invalidChannelMounts(err)
		}
		mounts.LegacyResourceFingerprint = fingerprint
	}
	mounts.Resources, mounts.GitRepository, record.GitTokenEnvelope = resources, nil, nil
	return nil
}

func (s *localService) sealChannelResource(ctx context.Context, record db.ChannelRecord, current []channelResourceMount, value normalizedChannelResource) (channelResourceMount, error) {
	if value.mount.Git == nil {
		return value.mount, nil
	}
	previous := slices.IndexFunc(current, func(old channelResourceMount) bool { return old.Git != nil && old.Git.URL == value.mount.Git.URL })
	if !value.tokenSet && previous >= 0 {
		value.mount.TokenEnvelope = current[previous].TokenEnvelope
		return value.mount, nil
	}
	if value.token != "" {
		envelope, err := sealChannelSecret(ctx, s.cipher, record, value.token)
		if err != nil {
			return channelResourceMount{}, err
		}
		value.mount.TokenEnvelope = envelope
	}
	return value.mount, nil
}

func (r channelResourceMount) payload() map[string]any {
	if r.Git == nil {
		return map[string]any{"type": r.Type, "file_id": r.FileID, "mount_path": r.MountPath}
	}
	value := map[string]any{"type": r.Type, "url": r.Git.URL, "mount_path": r.Git.MountPath}
	if r.Git.Checkout != nil {
		value["checkout"] = r.Git.Checkout
	}
	return value
}

func channelResourceFingerprint(resources []channelResourceMount) (string, error) {
	data, err := json.Marshal(resources)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func channelSessionResources(ctx context.Context, cipher *secrets.Service, record db.ChannelRecord) (channelResourceBundle, error) {
	mounts := parseChannelMounts(record.Mounts)
	configured, err := configuredChannelResources(record)
	if err != nil {
		return channelResourceBundle{}, err
	}
	fingerprint, err := channelResourceFingerprint(configured)
	if err != nil {
		return channelResourceBundle{}, err
	}
	bundle := channelResourceBundle{VaultIDs: mounts.VaultIDs, Fingerprint: fingerprint, Resources: []map[string]any{}}
	if mounts.MemoryStoreID != "" {
		bundle.Resources = append(bundle.Resources, map[string]any{"type": "memory_store", "memory_store_id": mounts.MemoryStoreID})
	}
	for _, resource := range configured {
		value := resource.payload()
		if resource.TokenEnvelope != nil {
			if cipher == nil {
				return channelResourceBundle{}, errors.New("渠道资源凭据加密服务不可用")
			}
			token, err := openSealedSecret(ctx, cipher, record, *resource.TokenEnvelope)
			if err != nil {
				return channelResourceBundle{}, err
			}
			value["authorization_token"] = token
		}
		bundle.Resources = append(bundle.Resources, value)
	}
	return bundle, nil
}
