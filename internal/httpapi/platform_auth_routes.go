package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type CodeConfiguration struct {
	Charset             *string `json:"charset,omitempty"`
	Length              *int    `json:"length,omitempty"`
	ShowInputAfterDelay *int    `json:"show_input_after_delay,omitempty"`
	Value               *string `json:"value,omitempty"`
}

type SendMagicLinkResponse struct {
	FallbackCodeConfiguration *CodeConfiguration `json:"fallback_code_configuration"`
	Sent                      bool               `json:"sent"`
	SSOURL                    *string            `json:"sso_url"`
	MagicLinkIntentAvailable  *bool              `json:"magic_link_intent_available"`
}

type VerifyMagicLinkRequest struct {
	Credentials        *VerifyMagicLinkCredentials `json:"credentials,omitempty"`
	PlayIntegrityToken *string                     `json:"play_integrity_token,omitempty"`
	RecaptchaSiteKey   *string                     `json:"recaptcha_site_key,omitempty"`
	RecaptchaToken     *string                     `json:"recaptcha_token,omitempty"`
	Source             *string                     `json:"source,omitempty"`
}

type VerifyMagicLinkCredentials struct {
	Method              *string `json:"method,omitempty"`
	Code                *string `json:"code,omitempty"`
	EmailAddress        *string `json:"email_address,omitempty"`
	Nonce               *string `json:"nonce,omitempty"`
	EncodedEmailAddress *string `json:"encoded_email_address,omitempty"`
}

type AuthenticationState struct {
	Kind                      string             `json:"kind"`
	Account                   *Account           `json:"account,omitempty"`
	Email                     *string            `json:"email,omitempty"`
	FallbackCodeConfiguration *CodeConfiguration `json:"fallback_code_configuration,omitempty"`
}

type VerifyResponse struct {
	Success                   bool                 `json:"success"`
	Created                   *bool                `json:"created,omitempty"`
	Account                   *Account             `json:"account,omitempty"`
	Secret                    *string              `json:"secret,omitempty"`
	SSOURL                    *string              `json:"sso_url,omitempty"`
	State                     *AuthenticationState `json:"state,omitempty"`
	Email                     *string              `json:"email,omitempty"`
	FallbackCodeConfiguration *CodeConfiguration   `json:"fallback_code_configuration,omitempty"`
}

type EmptyResponseWithSuccess struct {
	Success bool `json:"success"`
}

type loginMethodsResponse struct {
	Methods []string `json:"methods"`
}

type platformMagicLinkStore interface {
	bootstrapAccountStore
	FindOrCreateUserContextByEmail(ctx context.Context, email string) (userUUID string, orgUUID string, err error)
	ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error)
}

func RegisterPlatformEmailLoginRoutes(r chi.Router, store OrganizationStore, sessions platformsession.Store) {
	r.Get("/api/auth/login_methods", handleAuthLoginMethods)
	r.Post("/api/auth/send_magic_link", handleSendMagicLink)
	r.Post("/api/auth/verify_magic_link", handleVerifyMagicLink(store, sessions, false))
	r.Post("/api/auth/logout", handleWebLogout(store, sessions))
	r.Post("/auth/send_magic_link", handleSendMagicLink)
	r.Post("/auth/verify_magic_link", handleVerifyMagicLink(store, sessions, true))
	r.Post("/auth/logout", handleAndroidLogout(store, sessions))
}

func handleAuthLoginMethods(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, loginMethodsResponse{Methods: []string{"google", "magic_link"}})
}

func handleSendMagicLink(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}
	writeJSON(w, http.StatusOK, SendMagicLinkResponse{Sent: true})
}

func handleVerifyMagicLink(store OrganizationStore, sessions platformsession.Store, androidShape bool) http.HandlerFunc {
	magicLinkStore, _ := store.(platformMagicLinkStore)
	return func(w http.ResponseWriter, r *http.Request) {
		if magicLinkStore == nil || sessions == nil {
			internalError(w, "organization store is not configured")
			return
		}

		request := readVerifyMagicLinkRequest(r)
		userUUID, orgUUID, err := magicLinkStore.FindOrCreateUserContextByEmail(r.Context(), verifyMagicLinkEmail(request))
		if err != nil {
			internalError(w, "failed to verify magic link")
			return
		}

		account, selectedOrgUUID, err := buildBootstrapAccount(r.Context(), magicLinkStore, userUUID, orgUUID)
		if err != nil {
			internalError(w, "failed to load verified account")
			return
		}

		created := true
		sessionKey := "sk-ant-sid-session-key-" + uuid.NewString()
		expiresAt := time.Now().UTC().Add(time.Duration(25920000) * time.Second)
		session, err := magicLinkStore.ResolvePlatformSessionIdentity(r.Context(), platformsession.CreateInput{
			SessionKey: sessionKey,
			UserUUID:   account.UUID,
			OrgUUID:    selectedOrgUUID,
			ExpiresAt:  &expiresAt,
		})
		if err != nil {
			internalError(w, "failed to create session")
			return
		}
		if err := sessions.Save(r.Context(), sessionKey, session); err != nil {
			internalError(w, "failed to create session")
			return
		}

		setSessionCookies(w, sessionKey, selectedOrgUUID)
		response := VerifyResponse{
			Success: true,
			Created: &created,
			Account: &account,
		}
		if androidShape {
			response.Secret = &sessionKey
			response.State = &AuthenticationState{Kind: "authenticated", Account: &account}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func handleWebLogout(store OrganizationStore, sessions platformsession.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deleteCurrentSession(r, sessions); err != nil {
			internalError(w, "failed to logout")
			return
		}
		clearSessionCookies(w)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func handleAndroidLogout(store OrganizationStore, sessions platformsession.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deleteCurrentSession(r, sessions); err != nil {
			internalError(w, "failed to logout")
			return
		}
		clearSessionCookies(w)
		writeJSON(w, http.StatusOK, EmptyResponseWithSuccess{Success: true})
	}
}

func readVerifyMagicLinkRequest(r *http.Request) VerifyMagicLinkRequest {
	var request VerifyMagicLinkRequest
	if r.Body == nil {
		return request
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil || len(strings.TrimSpace(string(body))) == 0 {
		return request
	}
	_ = json.Unmarshal(body, &request)
	return request
}

func verifyMagicLinkEmail(request VerifyMagicLinkRequest) string {
	if request.Credentials == nil {
		return ""
	}
	if request.Credentials.EmailAddress != nil {
		return strings.TrimSpace(*request.Credentials.EmailAddress)
	}
	if request.Credentials.EncodedEmailAddress != nil {
		return decodeMagicLinkEmail(*request.Credentials.EncodedEmailAddress)
	}
	return ""
}

func decodeMagicLinkEmail(encoded string) string {
	value := strings.TrimSpace(encoded)
	if value == "" {
		return ""
	}
	if unescaped, err := url.QueryUnescape(value); err == nil {
		value = unescaped
	}
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			text := strings.TrimSpace(string(decoded))
			if strings.Contains(text, "@") {
				return text
			}
		}
	}
	if strings.Contains(value, "@") {
		return value
	}
	return ""
}

func setSessionCookies(w http.ResponseWriter, sessionKey string, orgUUID string) {
	const maxAge = 25920000
	http.SetCookie(w, &http.Cookie{Name: "lastActiveOrg", Value: orgUUID, Path: "/", MaxAge: maxAge})
	http.SetCookie(w, &http.Cookie{Name: "sessionKey", Value: sessionKey, Path: "/", MaxAge: maxAge})
}

func clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{"lastActiveOrg", "sessionKey"} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
	}
}

func deleteCurrentSession(r *http.Request, sessions platformsession.Store) error {
	cookie, err := r.Cookie("sessionKey")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil
	}
	sessionKey := strings.TrimSpace(cookie.Value)
	if sessions != nil {
		if err := sessions.Delete(r.Context(), sessionKey); err != nil {
			return err
		}
	}
	return nil
}
