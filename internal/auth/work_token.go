package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"strings"
)

type WorkTokenScope struct {
	WorkspaceUUID string
	EnvironmentID string
	WorkID        string
}

func NewWorkSecret(scope WorkTokenScope) (string, error) {
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		return "", err
	}
	token := strings.Join([]string{"oma_work", scope.WorkspaceUUID, scope.EnvironmentID, scope.WorkID, base64.RawURLEncoding.EncodeToString(material)}, ".")
	return workSecret(token), nil
}

func workSecret(token string) string {
	payload, _ := json.Marshal(struct {
		SessionsToken string `json:"sessions_token"`
	}{token})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func ParseWorkToken(token string) (WorkTokenScope, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != "oma_work" || len(parts[1]) != 36 || !strings.HasPrefix(parts[2], "env_") || !strings.HasPrefix(parts[3], "work_") {
		return WorkTokenScope{}, false
	}
	random, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil || len(random) != 32 {
		return WorkTokenScope{}, false
	}
	return WorkTokenScope{parts[1], parts[2], parts[3]}, true
}

func MatchesWorkSecret(token string, stored *string) bool {
	return stored != nil && subtle.ConstantTimeCompare([]byte(workSecret(token)), []byte(*stored)) == 1
}
