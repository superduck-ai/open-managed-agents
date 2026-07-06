package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const platformEnvironmentTokenExpirySeconds = 60 * 60 * 24 * 365

type platformEnvironmentToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

type platformEnvironmentTokenPagination struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

type platformEnvironmentTokenListResponse struct {
	Data       []platformEnvironmentToken         `json:"data"`
	Pagination platformEnvironmentTokenPagination `json:"pagination"`
}

type platformEnvironmentTokenStore struct {
	mu     sync.Mutex
	tokens map[string][]platformEnvironmentToken
}

var defaultPlatformEnvironmentTokenStore = &platformEnvironmentTokenStore{
	tokens: map[string][]platformEnvironmentToken{},
}

func RegisterOrganizationOAuthEnvironmentRoutes(r chi.Router) {
	r.Get("/environments/{environmentId}/tokens", handleListPlatformEnvironmentTokens)
	r.Post("/environments/{environmentId}/tokens", handleCreatePlatformEnvironmentToken)
}

func handleListPlatformEnvironmentTokens(w http.ResponseWriter, r *http.Request) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return
	}
	envID := chi.URLParam(r, "environmentId")
	tokens := defaultPlatformEnvironmentTokenStore.list(orgUUID, envID)
	writeJSON(w, http.StatusOK, platformEnvironmentTokenListResponse{
		Data: tokens,
		Pagination: platformEnvironmentTokenPagination{
			Total:   len(tokens),
			Limit:   100,
			Offset:  0,
			HasMore: false,
		},
	})
}

func handleCreatePlatformEnvironmentToken(w http.ResponseWriter, r *http.Request) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return
	}
	envID := chi.URLParam(r, "environmentId")
	body := readPlatformEnvironmentTokenCreateBody(r)
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(platformEnvironmentTokenExpirySeconds) * time.Second)
	token := platformEnvironmentToken{
		ID:        uuid.NewString(),
		Name:      platformEnvironmentTokenName(body),
		CreatedAt: formatJSISOString(now),
		ExpiresAt: formatJSISOString(expiresAt),
	}
	defaultPlatformEnvironmentTokenStore.create(orgUUID, envID, token)

	accessToken, err := newPlatformEnvironmentAccessToken()
	if err != nil {
		internalError(w, "failed to create environment token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"expires_in":   platformEnvironmentTokenExpirySeconds,
		"authorization_details": []map[string]any{
			{
				"type":           "ccr_env",
				"actions":        []string{"poll", "list", "stats"},
				"environment_id": envID,
			},
		},
	})
}

func (s *platformEnvironmentTokenStore) list(orgUUID string, envID string) []platformEnvironmentToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens := s.tokens[platformEnvironmentTokenKey(orgUUID, envID)]
	out := make([]platformEnvironmentToken, len(tokens))
	copy(out, tokens)
	return out
}

func (s *platformEnvironmentTokenStore) create(orgUUID string, envID string, token platformEnvironmentToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := platformEnvironmentTokenKey(orgUUID, envID)
	s.tokens[key] = append([]platformEnvironmentToken{token}, s.tokens[key]...)
}

func platformEnvironmentTokenKey(orgUUID string, envID string) string {
	return orgUUID + ":" + envID
}

func readPlatformEnvironmentTokenCreateBody(r *http.Request) map[string]any {
	if r.Body == nil {
		return map[string]any{}
	}
	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return map[string]any{}
	}
	return body
}

func platformEnvironmentTokenName(body map[string]any) string {
	if raw, ok := body["name"].(string); ok {
		if name := strings.TrimSpace(raw); name != "" {
			return name
		}
	}
	return "Environment key"
}

func newPlatformEnvironmentAccessToken() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "sk-ant-oat01-" + base64.RawURLEncoding.EncodeToString(buf), nil
}
