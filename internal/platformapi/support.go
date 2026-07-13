package platformapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
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

func optionalStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalTimeString(value *time.Time) any {
	if value == nil {
		return nil
	}
	return isoTime(*value)
}

func isoTime(value time.Time) string {
	if value.IsZero() {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatJSISOString(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func internalError(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": message})
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
