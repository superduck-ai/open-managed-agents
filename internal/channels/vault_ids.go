package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *localService) channelVaultIDs(ctx context.Context, principal auth.Principal, ids []string, input channelMutation) ([]string, error) {
	if len(input.VaultIDs) > 0 && len(input.VaultID) > 0 {
		return nil, apperr.New(apperr.InvalidArgument, "vault_ids 与 vault_id 不能同时提交", nil)
	}
	if len(input.VaultIDs) > 0 {
		if err := json.Unmarshal(input.VaultIDs, &ids); err != nil {
			return nil, apperr.New(apperr.InvalidArgument, "vault_ids 必须是字符串数组或 null", err)
		}
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
			if ids[i] == "" || slices.Contains(ids[:i], ids[i]) {
				return nil, apperr.New(apperr.InvalidArgument, "vault_ids 不能包含空标识或重复项", nil)
			}
		}
	} else if len(input.VaultID) > 0 {
		if len(ids) > 1 {
			return nil, apperr.New(apperr.InvalidArgument, "当前挂载多个Vault，请使用vault_ids更新", nil)
		}
		id, _, err := rawStringValue(input.VaultID)
		if err != nil {
			return nil, invalidChannelMounts(err)
		}
		ids = nil
		if id != "" {
			ids = []string{id}
		}
	}
	for _, id := range ids {
		vault, err := s.store.GetVault(ctx, principal.WorkspaceUUID, id)
		if errors.Is(err, db.ErrNotFound) || err == nil && (vault.ArchivedAt != nil || vault.DeletedAt != nil) {
			return nil, invalidVaultMount(err)
		}
		if err != nil {
			return nil, unavailable(err)
		}
	}
	return ids, nil
}
