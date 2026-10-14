package agentconfig

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
)

func Children(snapshot json.RawMessage) ([]json.RawMessage, error) {
	var root struct {
		Multiagent *struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(snapshot, &root); err != nil {
		return nil, err
	}
	if root.Multiagent == nil {
		return nil, nil
	}
	return root.Multiagent.Agents, nil
}
