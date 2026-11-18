package channels

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *localService) channelAgentVersion(ctx context.Context, principal auth.Principal, agentID string, current int, raw []byte) (int, error) {
	if len(raw) > 0 {
		var selected *int
		if err := json.Unmarshal(raw, &selected); err != nil || selected != nil && *selected < 1 {
			return 0, apperr.New(apperr.InvalidArgument, "agent_version 必须是正整数或 null", err)
		}
		current = 0
		if selected != nil {
			current = *selected
		}
	}
	if current == 0 {
		return 0, nil
	}
	agent, err := s.store.GetAgentVersion(ctx, principal.WorkspaceUUID, agentID, current)
	if errors.Is(err, db.ErrNotFound) || err == nil && agent.ArchivedAt != nil {
		return 0, apperr.New(apperr.InvalidArgument, "请选择当前智能体中存在且未归档的版本", err)
	}
	if err != nil {
		return 0, unavailable(err)
	}
	return current, nil
}
