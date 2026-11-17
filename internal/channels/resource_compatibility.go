package channels

import (
	"reflect"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func (r channelResourceMount) matches(actual omaSessionResource) bool {
	if r.Type != actual.Type {
		return false
	}
	if r.Git != nil {
		return reflect.DeepEqual(*r.Git, actual.GitRepositorySpec)
	}
	path, err := sandboxmount.FileBackingPath(r.MountPath)
	return err == nil && sandboxmount.SandboxPath(path) == actual.MountPath
}

func (w *channelWorker) compatibleMounts(session omaSession, personalID string) bool {
	if w.supervisor.personalVault != nil && session.Metadata["credential_binding_mode"] != "first_vault" {
		return false
	}
	mounts := parseChannelMounts(w.record.Mounts)
	if personalID != "" {
		if session.Metadata["personal_vault_id"] != personalID {
			return false
		}
		if !slices.Contains(mounts.VaultIDs, personalID) {
			mounts.VaultIDs = append(mounts.VaultIDs, personalID)
		}
	}
	if !slices.Equal(mounts.VaultIDs, session.VaultIDs) {
		return false
	}
	configured, err := configuredChannelResources(w.record)
	if err != nil {
		return false
	}
	fingerprint, err := channelResourceFingerprint(configured)
	if err != nil {
		return false
	}
	actualFingerprint := session.Metadata["channel_resources"]
	if actualFingerprint != "" && actualFingerprint != fingerprint || actualFingerprint == "" && mounts.Resources != nil && mounts.LegacyResourceFingerprint != fingerprint {
		return false
	}
	memoryID := ""
	actualGits := 0
	for _, resource := range session.Resources {
		switch resource.Type {
		case sessionresource.MemoryStoreType:
			if memoryID != "" {
				return false
			}
			memoryID = resource.MemoryStoreID
		case sessionresource.GitRepositoryType:
			actualGits++
		}
	}
	expectedGits := 0
	for _, resource := range configured {
		if resource.Git != nil {
			expectedGits++
		}
		if !slices.ContainsFunc(session.Resources, resource.matches) {
			return false
		}
	}
	return memoryID == mounts.MemoryStoreID && actualGits == expectedGits
}
