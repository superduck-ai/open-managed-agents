package platformapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const defaultConsoleWorkspaceID = "default"

type consoleAPIKeyStore interface {
	ListConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceID *string) ([]ConsoleAPIKey, error)
	CreateConsoleAPIKey(ctx context.Context, input CreateConsoleAPIKeyInput) (CreateConsoleAPIKeyResult, error)
	UpdateConsoleAPIKeyStatus(ctx context.Context, input UpdateConsoleAPIKeyStatusInput) (ConsoleAPIKey, error)
	CountConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceID string) (int, error)
}

type consoleWorkspaceLister interface {
	ListConsoleWorkspaces(ctx context.Context, orgUUID string, includeArchived bool) ([]ConsoleWorkspace, error)
}

type consoleWorkspaceCreator interface {
	CreateConsoleWorkspace(ctx context.Context, input CreateConsoleWorkspaceInput) (ConsoleWorkspace, error)
}

type createConsoleWorkspaceRequest struct {
	Name         string `json:"name"`
	DisplayColor string `json:"display_color"`
	Color        string `json:"color"`
}

type createConsoleAPIKeyRequest struct {
	Name      string  `json:"name"`
	ExpiresAt *string `json:"expires_at"`
}

type updateConsoleAPIKeyRequest struct {
	Status string `json:"status"`
}

func RegisterConsoleOrganizationAPIKeyRoutes(r chi.Router, store OrganizationStore) {
	registerConsoleOrganizationAPIKeyRoutes(r, store)
}

func registerConsoleOrganizationAPIKeyRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/api_keys", handleListConsoleAPIKeys(store))
	r.Post("/workspaces", handleCreateConsoleWorkspace(store))
	r.Get("/workspaces/{workspaceId}/api_keys", handleListConsoleWorkspaceAPIKeys(store))
	r.Post("/workspaces/{workspaceId}/api_keys", handleCreateConsoleWorkspaceAPIKey(store))
	r.Post("/workspaces/{workspaceId}/api_keys/{apiKeyId}", handleUpdateConsoleWorkspaceAPIKey(store))
	r.Get("/workspaces/{workspaceId}/api_keys/policy", handleGetConsoleWorkspaceAPIKeyPolicy)
	r.Get("/workspaces/{workspaceId}/api_key_count", handleCountConsoleWorkspaceAPIKeys(store))
}

func handleListConsoleWorkspaces(store OrganizationStore) http.HandlerFunc {
	workspaceLister, _ := store.(consoleWorkspaceLister)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if workspaceLister == nil {
			internalError(w, "failed to list workspaces")
			return
		}
		includeArchived := r.URL.Query().Get("include_archived") == "true"
		workspaces, err := workspaceLister.ListConsoleWorkspaces(r.Context(), orgUUID, includeArchived)
		if err != nil {
			internalError(w, "failed to list workspaces")
			return
		}
		out := make([]map[string]any, 0, len(workspaces))
		for _, workspace := range workspaces {
			if isDefaultConsoleWorkspace(workspace) {
				continue
			}
			out = append(out, formatConsoleWorkspace(workspace))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func isDefaultConsoleWorkspace(workspace ConsoleWorkspace) bool {
	return strings.EqualFold(strings.TrimSpace(workspace.Name), defaultConsoleWorkspaceID)
}

func handleCreateConsoleWorkspace(store OrganizationStore) http.HandlerFunc {
	workspaceCreator, _ := store.(consoleWorkspaceCreator)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if workspaceCreator == nil {
			internalError(w, "failed to create workspace")
			return
		}
		body, err := readRequiredJSON[createConsoleWorkspaceRequest](r, false)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "workspace name is required"})
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "workspace name is required"})
			return
		}
		displayColor := strings.TrimSpace(body.DisplayColor)
		if displayColor == "" {
			displayColor = strings.TrimSpace(body.Color)
		}
		if displayColor == "" {
			displayColor = "#9B87F5"
		}
		workspace, err := workspaceCreator.CreateConsoleWorkspace(r.Context(), CreateConsoleWorkspaceInput{
			OrgUUID:      orgUUID,
			Name:         name,
			DisplayColor: displayColor,
			Color:        displayColor,
		})
		if err != nil {
			internalError(w, "failed to create workspace")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleWorkspace(workspace))
	}
}

func handleListConsoleAPIKeys(store OrganizationStore) http.HandlerFunc {
	apiKeyStore, _ := store.(consoleAPIKeyStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if apiKeyStore == nil {
			internalError(w, "failed to list api keys")
			return
		}
		keys, err := apiKeyStore.ListConsoleAPIKeys(r.Context(), orgUUID, nil)
		if err != nil {
			internalError(w, "failed to list api keys")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleAPIKeys(keys))
	}
}

func handleListConsoleWorkspaceAPIKeys(store OrganizationStore) http.HandlerFunc {
	apiKeyStore, _ := store.(consoleAPIKeyStore)
	workspaceLister, _ := store.(consoleWorkspaceLister)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		workspaceID, ok := consoleWorkspaceIDFromRequest(w, r, workspaceLister, orgUUID)
		if !ok {
			return
		}
		if apiKeyStore == nil {
			internalError(w, "failed to list api keys")
			return
		}
		keys, err := apiKeyStore.ListConsoleAPIKeys(r.Context(), orgUUID, &workspaceID)
		if err != nil {
			internalError(w, "failed to list api keys")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleAPIKeys(keys))
	}
}

func handleCreateConsoleWorkspaceAPIKey(store OrganizationStore) http.HandlerFunc {
	apiKeyStore, _ := store.(consoleAPIKeyStore)
	workspaceLister, _ := store.(consoleWorkspaceLister)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		workspaceID, ok := consoleWorkspaceIDFromRequest(w, r, workspaceLister, orgUUID)
		if !ok {
			return
		}
		if apiKeyStore == nil {
			internalError(w, "failed to create api key")
			return
		}
		body, err := readRequiredJSON[createConsoleAPIKeyRequest](r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "request body must match CreateConsoleAPIKeyRequest",
			})
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "name is required",
			})
			return
		}
		if len(name) > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "name must be 500 characters or fewer",
			})
			return
		}
		expiresAt, err := parseConsoleAPIKeyExpiresAt(body.ExpiresAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "expires_at must be an RFC3339 timestamp or null",
			})
			return
		}
		auth := authFromContext(r.Context())
		var createdByUserUUID *string
		if auth != nil {
			createdByUserUUID = &auth.Account.UUID
		}
		result, err := apiKeyStore.CreateConsoleAPIKey(r.Context(), CreateConsoleAPIKeyInput{
			OrgUUID:           orgUUID,
			WorkspaceID:       workspaceID,
			Name:              name,
			ExpiresAt:         expiresAt,
			CreatedByUserUUID: createdByUserUUID,
		})
		if err != nil {
			internalError(w, "failed to create api key")
			return
		}
		response := formatConsoleAPIKey(result.APIKey)
		response["raw_key"] = result.RawKey
		writeJSON(w, http.StatusOK, response)
	}
}

func handleUpdateConsoleWorkspaceAPIKey(store OrganizationStore) http.HandlerFunc {
	apiKeyStore, _ := store.(consoleAPIKeyStore)
	workspaceLister, _ := store.(consoleWorkspaceLister)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		workspaceID, ok := consoleWorkspaceIDFromRequest(w, r, workspaceLister, orgUUID)
		if !ok {
			return
		}
		apiKeyID := strings.TrimSpace(chi.URLParam(r, "apiKeyId"))
		if apiKeyID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "api_key_id is required",
			})
			return
		}
		if apiKeyStore == nil {
			internalError(w, "failed to update api key")
			return
		}
		body, err := readRequiredJSON[updateConsoleAPIKeyRequest](r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "request body must match UpdateConsoleAPIKeyRequest",
			})
			return
		}
		status := normalizeConsoleAPIKeyStatus(body.Status)
		if status == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "status must be active, inactive, or archived",
			})
			return
		}
		key, err := apiKeyStore.UpdateConsoleAPIKeyStatus(r.Context(), UpdateConsoleAPIKeyStatusInput{
			OrgUUID:     orgUUID,
			WorkspaceID: workspaceID,
			APIKeyID:    apiKeyID,
			Status:      status,
		})
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "api_key_not_found"})
				return
			}
			internalError(w, "failed to update api key")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleAPIKey(key))
	}
}

func handleGetConsoleWorkspaceAPIKeyPolicy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"max_api_key_age_hours": nil,
	})
}

func handleCountConsoleWorkspaceAPIKeys(store OrganizationStore) http.HandlerFunc {
	apiKeyStore, _ := store.(consoleAPIKeyStore)
	workspaceLister, _ := store.(consoleWorkspaceLister)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		workspaceID, ok := consoleWorkspaceIDFromRequest(w, r, workspaceLister, orgUUID)
		if !ok {
			return
		}
		if apiKeyStore == nil {
			internalError(w, "failed to count api keys")
			return
		}
		count, err := apiKeyStore.CountConsoleAPIKeys(r.Context(), orgUUID, workspaceID)
		if err != nil {
			internalError(w, "failed to count api keys")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": count})
	}
}

func consoleWorkspaceIDFromRequest(w http.ResponseWriter, r *http.Request, lister consoleWorkspaceLister, orgUUID string) (string, bool) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if workspaceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": "workspace_id is required",
		})
		return "", false
	}
	if workspaceID == defaultConsoleWorkspaceID || lister == nil {
		return workspaceID, true
	}
	workspaces, err := lister.ListConsoleWorkspaces(r.Context(), orgUUID, true)
	if err != nil {
		internalError(w, "failed to load workspaces")
		return "", false
	}
	for _, workspace := range workspaces {
		if workspace.UUID == workspaceID {
			return workspaceID, true
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "workspace not found"})
	return "", false
}

func parseConsoleAPIKeyExpiresAt(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, trimmed)
		if err != nil {
			return nil, err
		}
	}
	parsed = parsed.UTC().Truncate(time.Millisecond)
	return &parsed, nil
}

func normalizeConsoleAPIKeyStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "active", "inactive", "archived":
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return ""
	}
}

func formatConsoleAPIKeys(keys []ConsoleAPIKey) []map[string]any {
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, formatConsoleAPIKey(key))
	}
	return out
}

func formatConsoleAPIKey(key ConsoleAPIKey) map[string]any {
	status := key.Status
	if status == "" {
		status = "active"
		if key.ArchivedAt != nil {
			status = "archived"
		}
	}
	var createdBy any
	if key.CreatedByUserUUID != nil && strings.TrimSpace(*key.CreatedByUserUUID) != "" {
		createdBy = map[string]any{
			"id":   strings.TrimSpace(*key.CreatedByUserUUID),
			"type": "user",
		}
	}
	var workspaceID any = key.WorkspaceID
	if key.WorkspaceID == defaultConsoleWorkspaceID {
		workspaceID = nil
	}
	return map[string]any{
		"type":               "api_key",
		"id":                 key.ID,
		"workspace_id":       workspaceID,
		"name":               key.Name,
		"key_prefix":         key.KeyPrefix,
		"key_suffix":         key.KeySuffix,
		"partial_key_hint":   key.KeyPrefix + "..." + key.KeySuffix,
		"created_by":         createdBy,
		"created_by_user_id": optionalStringValue(key.CreatedByUserUUID),
		"last_used_at":       optionalTimeString(key.LastUsedAt),
		"expires_at":         optionalTimeString(key.ExpiresAt),
		"archived_at":        optionalTimeString(key.ArchivedAt),
		"status":             status,
		"created_at":         isoTime(key.CreatedAt),
		"updated_at":         isoTime(key.UpdatedAt),
	}
}

func formatConsoleWorkspace(workspace ConsoleWorkspace) map[string]any {
	externalMapping := any(nil)
	if len(workspace.ExternalMapping) > 0 {
		externalMapping = workspace.ExternalMapping
	}
	tags := workspace.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	displayColor := workspace.DisplayColor
	if displayColor == "" {
		displayColor = workspace.Color
	}
	if displayColor == "" {
		displayColor = "#9B87F5"
	}
	color := workspace.Color
	if color == "" {
		color = displayColor
	}
	return map[string]any{
		"id":                       workspace.UUID,
		"type":                     "workspace",
		"name":                     workspace.Name,
		"display_color":            displayColor,
		"color":                    color,
		"created_at":               isoTime(workspace.CreatedAt),
		"updated_at":               isoTime(workspace.UpdatedAt),
		"tags":                     tags,
		"external_mapping":         externalMapping,
		"external_key_id":          optionalStringValue(workspace.ExternalKeyID),
		"compartment_id":           workspace.UUID,
		"inference_data_retention": false,
		"archived_at":              optionalTimeString(workspace.ArchivedAt),
	}
}
