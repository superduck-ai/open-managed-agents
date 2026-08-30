package platformapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"

	"github.com/go-chi/chi/v5"
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

type SendMagicLinkRequest struct {
	EmailAddress string `json:"email_address"`
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

type emailLoginService interface {
	RequestEmailLogin(ctx context.Context, email string) error
	VerifyEmailLogin(ctx context.Context, email, code string) (userID string, orgUUID string, err error)
}

type platformMagicLinkStore interface {
	bootstrapAccountStore
	ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error)
}

func RegisterPlatformEmailLoginRoutes(r chi.Router, store platformMagicLinkStore, authProvider emailLoginService, sessions platformsession.Store) {
	r.Get("/api/auth/login_methods", handleAuthLoginMethods)
	r.Post("/api/auth/send_magic_link", handleSendMagicLink(authProvider))
	r.Post("/api/auth/verify_magic_link", handleVerifyMagicLink(store, authProvider, sessions, false))
	r.Post("/api/auth/logout", handleWebLogout(store, sessions))
	r.Post("/auth/send_magic_link", handleSendMagicLink(authProvider))
	r.Post("/auth/verify_magic_link", handleVerifyMagicLink(store, authProvider, sessions, true))
	r.Post("/auth/logout", handleAndroidLogout(store, sessions))
}

func handleAuthLoginMethods(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, loginMethodsResponse{Methods: []string{"google", "magic_link"}})
}

func handleSendMagicLink(authProvider emailLoginService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := httpapi.DecodeObjectBodyAs[SendMagicLinkRequest](w, r, 64*1024)
		if err != nil {
			writeEmailLoginError(w, r, invalidEmailLoginRequest(err))
			return
		}
		if err := authProvider.RequestEmailLogin(r.Context(), request.EmailAddress); err != nil {
			writeEmailLoginError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, SendMagicLinkResponse{Sent: true})
	}
}

func handleVerifyMagicLink(store platformMagicLinkStore, authProvider emailLoginService, sessions platformsession.Store, androidShape bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil || sessions == nil {
			internalError(w, "organization store is not configured")
			return
		}

		request, err := httpapi.DecodeObjectBodyAs[VerifyMagicLinkRequest](w, r, 64*1024)
		if err != nil {
			writeEmailLoginError(w, r, invalidEmailLoginRequest(err))
			return
		}
		userUUID, orgUUID, err := authProvider.VerifyEmailLogin(r.Context(), verifyMagicLinkEmail(*request), verifyMagicLinkCode(*request))
		if err != nil {
			writeEmailLoginError(w, r, err)
			return
		}

		account, selectedOrgUUID, err := buildBootstrapAccount(r.Context(), store, userUUID, orgUUID)
		if err != nil {
			internalError(w, "failed to load verified account")
			return
		}

		created := true
		sessionKey := "sk-ant-sid-session-key-" + uuid.NewV4().String()
		expiresAt := time.Now().UTC().Add(time.Duration(25920000) * time.Second)
		session, err := store.ResolvePlatformSessionIdentity(r.Context(), platformsession.CreateInput{
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

func verifyMagicLinkEmail(request VerifyMagicLinkRequest) string {
	if request.Credentials == nil {
		return ""
	}
	if request.Credentials.EmailAddress != nil {
		return *request.Credentials.EmailAddress
	}
	if request.Credentials.EncodedEmailAddress != nil {
		return decodeMagicLinkEmail(*request.Credentials.EncodedEmailAddress)
	}
	return ""
}

func verifyMagicLinkCode(request VerifyMagicLinkRequest) string {
	if request.Credentials == nil || request.Credentials.Code == nil {
		return ""
	}
	return *request.Credentials.Code
}

func writeEmailLoginError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		internalError(w, "email login failed")
		return
	}
	status := http.StatusInternalServerError
	code := "api_error"
	switch appErr.Kind {
	case apperr.InvalidArgument:
		status, code = http.StatusBadRequest, "invalid_request_error"
	case apperr.Unauthenticated:
		status, code = http.StatusUnauthorized, "authentication_error"
	case apperr.RateLimited:
		status, code = http.StatusTooManyRequests, "rate_limit_error"
	case apperr.Unavailable:
		status, code = http.StatusServiceUnavailable, "api_error"
	}
	writeJSON(w, status, map[string]any{
		"error":      code,
		"message":    appErr.PublicMessage,
		"request_id": httpapi.RequestID(r.Context()),
	})
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
	http.SetCookie(w, &http.Cookie{Name: "lastActiveOrg", Value: orgUUID, Path: "/", MaxAge: maxAge, HttpOnly: false, Secure: false, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "sessionKey", Value: sessionKey, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: false, SameSite: http.SameSiteLaxMode})
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
