package environments

import (
	"encoding/json"
	"path"
)

const (
	rcloneOutputsDestination    = "/mnt/user-data/outputs"
	sessionOutputsPath          = "/mnt/session/outputs"
	agentScoreDataAlias         = "/data"
	agentScoreOutputAlias       = "/output"
	agentScoreEvaluationRoleKey = "evaluation_role"
	agentScoreEvaluationRole    = "agent_score"
)

func sessionOutputsAliasCommand(target, alias string) string {
	quotedAlias := shellQuote(alias)
	quotedTarget := shellQuote(target)
	return "mkdir -p " + shellQuote(path.Dir(alias)) +
		" && if [ -L " + quotedAlias + " ]; then test \"$(readlink " + quotedAlias + ")\" = " + quotedTarget +
		"; else if [ -e " + quotedAlias + " ]; then rmdir " + quotedAlias + " || exit 1; fi; ln -s " + quotedTarget + " " + quotedAlias + "; fi"
}

func agentScoreContractAliasCommands() []string {
	return []string{
		sessionOutputsAliasCommand(rcloneUploadsDestination+"/data", agentScoreDataAlias),
		sessionOutputsAliasCommand(rcloneOutputsDestination, agentScoreOutputAlias),
	}
}

func sessionRequestsAgentScorePaths(metadata json.RawMessage) bool {
	if len(metadata) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(metadata, &fields) != nil || fields == nil {
		return false
	}
	raw, ok := fields[agentScoreEvaluationRoleKey]
	if !ok {
		return false
	}
	var role string
	return json.Unmarshal(raw, &role) == nil && role == agentScoreEvaluationRole
}
