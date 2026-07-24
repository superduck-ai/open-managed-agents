package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/aiupstream"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"

	"github.com/go-chi/chi/v5"
)

const (
	defaultAnthropicMaxTokens = 16384
	anthropicAPIVersion       = "2023-06-01"
)

var errWorkbenchModelRequired = errors.New("workbench model selection is required")

type OrganizationStore interface{}

type workbenchAuthContext struct {
	Account workbenchAccount
}

type workbenchAccount struct {
	TaggedID     string
	UUID         string
	EmailAddress string
	FullName     *string
	DisplayName  *string
}

func RegisterOrgWorkbenchRoutes(
	r chi.Router,
	store OrganizationStore,
	upstream config.AnthropicUpstreamConfig,
	catalog modelcatalog.Reader,
) {
	registerOrgWorkbenchRoutes(r, store, upstream, catalog)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	httpapi.WriteJSON(w, status, body)
}

func readJSONObject(r *http.Request) (map[string]any, error) {
	value, err := readRequiredJSON[map[string]any](r, false)
	if err != nil || value == nil {
		return nil, err
	}
	return value, nil
}

func readRequiredJSON[T any](r *http.Request, disallowUnknownFields bool) (T, error) {
	var zero T
	if r.Body == nil || r.Body == http.NoBody {
		return zero, io.EOF
	}
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, err
	}
	return value, nil
}

func authFromContext(ctx context.Context) *workbenchAuthContext {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return nil
	}
	accountID := firstNonEmpty(principal.UserExternalID, principal.APIKeyExternalID, principal.PlatformSessionExternalID)
	displayName := accountID
	if displayName == "" {
		displayName = "local"
	}
	return &workbenchAuthContext{
		Account: workbenchAccount{
			TaggedID:     accountID,
			UUID:         accountID,
			EmailAddress: accountID,
			DisplayName:  &displayName,
		},
	}
}

func visibleOrgUUID(w http.ResponseWriter, r *http.Request) (string, bool) {
	orgUUID := strings.TrimSpace(chi.URLParam(r, "orgUuid"))
	principal, ok := auth.PrincipalFromContext(r.Context())
	if orgUUID == "" || !ok {
		organizationNotFound(w)
		return "", false
	}
	if principalCanSeeOrg(principal, orgUUID) {
		return orgUUID, true
	}
	if alias := auth.PlatformMirrorOrganizationAliasFromContext(r.Context()); alias != "" && alias == orgUUID && isPlatformClaudeHost(r.Host) {
		if localOrgUUID := strings.TrimSpace(principal.OrganizationUUID); localOrgUUID != "" {
			return localOrgUUID, true
		}
	}
	organizationNotFound(w)
	return "", false
}

func principalCanSeeOrg(principal auth.Principal, orgUUID string) bool {
	orgUUID = strings.TrimSpace(orgUUID)
	if orgUUID == "" {
		return false
	}
	return orgUUID == strings.TrimSpace(principal.OrganizationUUID) ||
		orgUUID == strings.TrimSpace(principal.OrganizationExternalID)
}

func organizationNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "organization not found"})
}

func isPlatformClaudeHost(requestHost string) bool {
	host := strings.TrimSpace(strings.ToLower(requestHost))
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	return host == "platform.claude.com" || strings.HasSuffix(host, ".platform.claude.com")
}

func platformClaudeOptionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func formatJSISOString(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func proxyMessagesAnthropicToken(upstream config.AnthropicUpstreamConfig) string {
	return strings.TrimSpace(upstream.APIKey)
}

func resolveWorkbenchModel(r *http.Request, candidates ...string) (string, error) {
	catalog := workbenchModelCatalogFromRequest(r)
	if catalog == nil {
		return "", modelcatalog.ErrUnavailable
	}

	modelID := firstNonEmpty(candidates...)
	if modelID == "" {
		snapshot, err := catalog.Snapshot(r.Context())
		if err != nil {
			return "", modelcatalog.ErrUnavailable
		}
		modelID = snapshot.DefaultModelID
	}
	if modelID == "" {
		return "", errWorkbenchModelRequired
	}
	if err := catalog.ValidateModel(r.Context(), modelID); err != nil {
		if modelcatalog.IsUnknownModel(err) {
			return "", modelcatalog.ErrUnknownModel
		}
		return "", modelcatalog.ErrUnavailable
	}
	return modelID, nil
}

func writeWorkbenchModelSelectionError(w http.ResponseWriter, err error) {
	if modelcatalog.IsUnavailable(err) {
		writeProxyMessagesAnthropicError(w, http.StatusServiceUnavailable, "api_error", "Model catalog is unavailable")
		return
	}
	message := "Selected model is not available from the configured model catalog"
	if errors.Is(err, errWorkbenchModelRequired) {
		message = "A model selection is required"
	}
	writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", message)
}

func anthropicMessagesEndpoint(upstream config.AnthropicUpstreamConfig) (string, error) {
	if err := aiupstream.ValidateDeployment(upstream.BaseURL, upstream.APIKey); err != nil {
		return "", err
	}
	return aiupstream.Endpoint(upstream.BaseURL, "v1/messages", "")
}

func proxyMessagesStream(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, 32*1024)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			_, _ = w.Write(buffer[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func copyProxyMessagesResponseHeaders(dst http.Header, src http.Header) {
	for name, values := range src {
		if proxyMessagesResponseHeaderSkipped(name) {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func proxyMessagesResponseHeaderSkipped(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "content-encoding", "content-length", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func writeProxyMessagesAnthropicError(w http.ResponseWriter, status int, errorType string, message string) {
	if errorType == "" {
		errorType = "api_error"
	}
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errorType,
			"message": message,
		},
	})
}

func buildChatCompletionAnthropicTools(tools []any) []any {
	out := []any{}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rewritten := rewriteChatCompletionIntegrationTool(tool)
		if converted := buildChatCompletionAnthropicTool(rewritten); converted != nil {
			out = append(out, converted)
		}
	}
	return out
}

func rewriteChatCompletionIntegrationTool(tool map[string]any) map[string]any {
	rewritten := chatCloneMap(tool)
	integration := chatMapString(rewritten, "integration_name")
	name := chatMapString(rewritten, "name")
	if integration == "" || name == "" {
		return rewritten
	}
	rewritten["name"] = "mcp__" + integration + "__" + name
	delete(rewritten, "integration_name")
	delete(rewritten, "integrationName")
	delete(rewritten, "is_mcp_app")
	return rewritten
}

func buildChatCompletionAnthropicTool(tool map[string]any) map[string]any {
	name := chatNormalizeString(tool["name"])
	if name == "" {
		return nil
	}
	if chatNormalizeString(tool["type"]) == "web_search_v0" {
		out := map[string]any{"type": "web_search_20260209", "name": name}
		if maxUses := chatInt(tool["max_uses"]); maxUses > 0 {
			out["max_uses"] = maxUses
		} else if maxUses := chatInt(tool["maxUses"]); maxUses > 0 {
			out["max_uses"] = maxUses
		}
		return out
	}
	out := map[string]any{
		"name":         name,
		"type":         "custom",
		"input_schema": buildChatCompletionToolInputSchema(firstNonNil(tool["input_schema"], tool["inputSchema"])),
	}
	if description := chatNormalizeString(tool["description"]); description != "" {
		out["description"] = description
	}
	for _, key := range []string{"eager_input_streaming", "defer_loading", "strict"} {
		if value, ok := tool[key].(bool); ok {
			out[key] = value
		}
	}
	if value, ok := tool["eagerInputStreaming"].(bool); ok {
		out["eager_input_streaming"] = value
	}
	if value, ok := tool["deferLoading"].(bool); ok {
		out["defer_loading"] = value
	}
	return out
}

func buildChatCompletionToolInputSchema(value any) map[string]any {
	schema := map[string]any{"type": "object", "additionalProperties": true}
	payload, ok := value.(map[string]any)
	if !ok {
		return schema
	}
	if properties, exists := payload["properties"]; exists {
		schema["properties"] = chatClone(properties)
	}
	if required := chatNormalizeStringArray(payload["required"], 0); len(required) > 0 {
		schema["required"] = required
	}
	for key, item := range payload {
		if key == "type" || key == "properties" || key == "required" {
			continue
		}
		schema[key] = chatClone(item)
	}
	return schema
}

func chatArrayFromValue(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return []any{}
	}
}

func chatCloneMap(value map[string]any) map[string]any {
	cloned, ok := chatClone(value).(map[string]any)
	if !ok || cloned == nil {
		return map[string]any{}
	}
	return cloned
}

func chatClone(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

func chatMapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return chatNormalizeString(values[key])
}

func chatNormalizeString(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func chatRawString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func chatNormalizeStringArray(value any, depth int) []string {
	if values, ok := value.([]string); ok {
		out := []string{}
		for _, item := range values {
			if normalized := chatNormalizeString(item); normalized != "" {
				out = append(out, normalized)
			}
		}
		return out
	}
	if values, ok := value.([]any); ok {
		out := []string{}
		for _, item := range values {
			if normalized := chatNormalizeString(item); normalized != "" {
				out = append(out, normalized)
			}
		}
		return out
	}
	normalized := chatNormalizeString(value)
	if normalized == "" {
		return []string{}
	}
	if depth >= 2 {
		return []string{normalized}
	}
	var parsed any
	if err := json.Unmarshal([]byte(normalized), &parsed); err == nil {
		switch parsed.(type) {
		case []any, string:
			return chatNormalizeStringArray(parsed, depth+1)
		}
	}
	return []string{normalized}
}

func chatInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func chatPositiveInt(value any) int {
	n := chatInt(value)
	if n > 0 {
		return n
	}
	return 0
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
