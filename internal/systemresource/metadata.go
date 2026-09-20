// Package systemresource identifies resources owned exclusively by server
// workflows and therefore hidden from public selection and mutation surfaces.
package systemresource

import "encoding/json"

const (
	DreamDefaultAgentKind       = "dream_default_agent"
	DreamDefaultEnvironmentKind = "dream_default_environment"
)

func Kind(metadata json.RawMessage) string {
	var value struct {
		InternalKind string `json:"internal_kind"`
	}
	if json.Unmarshal(metadata, &value) != nil {
		return ""
	}
	return value.InternalKind
}

func IsDreamDefaultAgent(metadata json.RawMessage) bool {
	return Kind(metadata) == DreamDefaultAgentKind
}

func IsDreamDefaultEnvironment(metadata json.RawMessage) bool {
	return Kind(metadata) == DreamDefaultEnvironmentKind
}

func IsReservedKind(kind string) bool {
	switch kind {
	case DreamDefaultAgentKind, DreamDefaultEnvironmentKind:
		return true
	default:
		return false
	}
}
