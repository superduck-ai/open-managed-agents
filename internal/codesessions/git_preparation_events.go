package codesessions

import (
	"encoding/json"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

// Only resource preparation metadata enters public history; ordinary Manager
// logs and Git stderr remain internal. Reuse system.message and the event stream.
func gitPreparationPublicFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	var data struct {
		Extra struct {
			ResourceType  string `json:"resource_type"`
			Status        string `json:"status"`
			URL           string `json:"url"`
			MountPath     string `json:"mount_path"`
			DurationMS    *int64 `json:"duration_ms"`
			FailureReason string `json:"failure_reason"`
		} `json:"extra"`
	}
	if json.Unmarshal(fields["data"], &data) != nil {
		return fields
	}
	resource := data.Extra
	if resource.ResourceType != "git_repository" || sessionresource.ValidateGitHubURL(resource.URL) != nil || sessionresource.ValidateGitHubMountPaths([]string{resource.MountPath}) != nil {
		return fields
	}
	labels := map[string]string{"started": "Preparing Git repository", "ready": "Git repository ready", "failed": "Git repository preparation failed"}
	label, ok := labels[resource.Status]
	if !ok {
		return fields
	}
	result := make(map[string]json.RawMessage)
	for _, key := range []string{"uuid", "created_at", "timestamp"} {
		if raw, ok := fields[key]; ok {
			result[key] = raw
		}
	}
	setRawJSONField(result, "type", "system.message")
	setRawJSONField(result, "subtype", "git_repository")
	setRawJSONField(result, "resource_status", resource.Status)
	setRawJSONField(result, "url", resource.URL)
	setRawJSONField(result, "mount_path", resource.MountPath)
	message := fmt.Sprintf("%s: %s → %s", label, resource.URL, resource.MountPath)
	if resource.Status != "started" && resource.DurationMS != nil && *resource.DurationMS >= 0 {
		setRawJSONField(result, "duration_ms", *resource.DurationMS)
		message += fmt.Sprintf(" (%.1f s)", float64(*resource.DurationMS)/1000)
	}
	reasons := map[string]string{
		"access_denied":   "Repository not found or access denied. Check the URL and token permissions; private repositories require a token.",
		"network_error":   "Could not connect to the Git server. Check the network and proxy settings.",
		"checkout_failed": "Could not check out the requested branch or commit. Check that it exists and is accessible.",
		"path_conflict":   "The repository directory is unavailable. Check the mount path, directory permissions, and available disk space.",
		"timed_out":       "Repository preparation timed out. Check the connection and try again.",
		"cancelled":       "Repository preparation was cancelled.",
		"unknown":         "Git did not complete repository preparation. Check the runtime logs for details.",
	}
	if reason, ok := reasons[resource.FailureReason]; resource.Status == "failed" && ok {
		setRawJSONField(result, "failure_reason", resource.FailureReason)
		message += ". " + reason
	}
	setRawJSONField(result, "content", []map[string]string{{"type": "text", "text": message}})
	return result
}
