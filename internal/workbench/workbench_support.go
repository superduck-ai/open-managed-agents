package workbench

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

const (
	defaultAnthropicMaxTokens = 16384
	anthropicAPIVersion       = "2023-06-01"
	chatFallbackModel         = "claude-haiku-4-5-20251001"
	miscDefaultChatModel      = "claude-sonnet-4-6"
)

var chatModelFallbacks = map[string]string{
	"claude-fable-5":                     chatFallbackModel,
	"claude-opus-4-8":                    chatFallbackModel,
	"claude-opus-4-1-20250805":           chatFallbackModel,
	"claude-opus-4-20250514":             chatFallbackModel,
	"claude-opus-4-5-20251101":           chatFallbackModel,
	"claude-opus-4-6":                    chatFallbackModel,
	"claude-opus-4-7":                    chatFallbackModel,
	"claude-sonnet-4-5-20250929":         chatFallbackModel,
	"claude-sonnet-4-6":                  chatFallbackModel,
	"claude-haiku-4-5-20251001":          chatFallbackModel,
	"claude-3-opus-20240229":             chatFallbackModel,
	"claude-3-5-sonnet-20241022":         chatFallbackModel,
	"claude-3-7-sonnet-20250219":         chatFallbackModel,
	"claude-sonnet-4-20250514":           chatFallbackModel,
	"claude-opus-4-1-20250805-claude-ai": chatFallbackModel,
}

var chatActiveModels = map[string]struct{}{
	miscDefaultChatModel: {},
	"claude-fable-5":     {},
	"claude-opus-4-8":    {},
	"claude-opus-4-7":    {},
	chatFallbackModel:    {},
}

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

func RegisterOrgWorkbenchRoutes(r chi.Router, store OrganizationStore) {
	registerOrgWorkbenchRoutes(r, store)
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

func proxyMessagesAnthropicToken() string {
	for _, key := range []string{"ANTHROPIC_UPSTREAM_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if token := strings.TrimSpace(os.Getenv(key)); token != "" {
			return token
		}
	}
	return ""
}

func anthropicMessagesEndpoint() (string, error) {
	baseURL := firstNonEmpty(os.Getenv("ANTHROPIC_UPSTREAM_BASE_URL"), os.Getenv("ANTHROPIC_BASE_URL"), "https://api.anthropic.com")
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	return parsed.String(), nil
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

func chatCompletionModel(model string) string {
	normalized := chatNormalizeString(model)
	if normalized == "" {
		return miscDefaultChatModel
	}
	if _, ok := chatActiveModels[normalized]; ok {
		return normalized
	}
	if canonical := chatModelWithoutReleaseDate(normalized); canonical != normalized {
		if _, ok := chatActiveModels[canonical]; ok {
			return canonical
		}
	}
	if _, ok := chatModelFallbacks[normalized]; ok {
		return miscDefaultChatModel
	}
	return normalized
}

func chatModelWithoutReleaseDate(model string) string {
	if len(model) < 9 || model[len(model)-9] != '-' {
		return model
	}
	suffix := model[len(model)-8:]
	if len(suffix) != 8 || !strings.HasPrefix(suffix, "20") {
		return model
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return model
		}
	}
	return model[:len(model)-9]
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
