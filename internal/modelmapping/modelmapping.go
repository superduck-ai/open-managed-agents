package modelmapping

import (
	"fmt"
	"strings"
)

// Resolve returns the deployment model ID configured for modelID. Source and
// target IDs are trimmed. Missing, blank, and identity mappings preserve the
// trimmed original ID.
func Resolve(modelID string, mappings map[string]string) string {
	modelID = strings.TrimSpace(modelID)
	mappedID := strings.TrimSpace(mappings[modelID])
	if mappedID == "" {
		return modelID
	}
	return mappedID
}

// Validate rejects ambiguous mappings that could resolve differently at
// separate API boundaries.
func Validate(mappings map[string]string) error {
	for sourceID, configuredTargetID := range mappings {
		if strings.TrimSpace(sourceID) != sourceID || sourceID == "" {
			return fmt.Errorf("model mapping source %q must be a non-empty model ID without surrounding whitespace", sourceID)
		}
		targetID := strings.TrimSpace(configuredTargetID)
		if targetID == "" || targetID == sourceID {
			continue
		}
		nextTargetID := strings.TrimSpace(mappings[targetID])
		if nextTargetID != "" && nextTargetID != targetID {
			return fmt.Errorf(
				"model mapping %q to %q is ambiguous because %q also maps to %q",
				sourceID,
				targetID,
				targetID,
				nextTargetID,
			)
		}
	}
	return nil
}
