package sessions

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func applySessionSystemOverride(agent db.Agent, raw json.RawMessage) (db.Agent, error) {
	if len(raw) == 0 {
		return agent, nil
	}
	if httpapi.IsJSONNull(raw) {
		agent.System = nil
		return agent, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || utf8.RuneCountInString(value) > 100000 {
		return db.Agent{}, errInvalidSessionSystemOverride
	}
	agent.System = &value
	return agent, nil
}
