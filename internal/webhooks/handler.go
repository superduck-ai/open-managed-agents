package webhooks

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

const (
	webhooksBeta       = "webhooks-2026-03-01"
	maxWebhookBodySize = 1 << 20
)

var supportedEndpointEventTypes = map[string]struct{}{
	"session.status_run_started":        {},
	"session.status_idled":              {},
	"session.status_rescheduled":        {},
	"session.status_terminated":         {},
	"session.deleted":                   {},
	"session.updated":                   {},
	"session.error":                     {},
	"session.thread_created":            {},
	"session.thread_status_running":     {},
	"session.thread_status_idle":        {},
	"session.thread_status_rescheduled": {},
	"session.thread_status_terminated":  {},
	"session.thread_idled":              {},
	"session.thread_terminated":         {},
	"session.outcome_evaluation_ended":  {},
	"vault.created":                     {},
	"vault.archived":                    {},
	"vault.deleted":                     {},
	"vault_credential.created":          {},
	"vault_credential.archived":         {},
	"vault_credential.deleted":          {},
	"vault_credential.refresh_failed":   {},
}

type Handler struct {
	cfg          config.WebhookConfig
	db           *db.DB
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

type webhookResponse struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	URL            string   `json:"url"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	EnabledEvents  []string `json:"enabled_events"`
	Status         string   `json:"status"`
	DisabledReason *string  `json:"disabled_reason"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	SigningSecret  *string  `json:"signing_secret,omitempty"`
}

type webhookPageResponse struct {
	Data     []webhookResponse `json:"data"`
	NextPage *string           `json:"next_page"`
}

type deleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type regenerateSigningSecretResponse struct {
	SigningSecret string `json:"signing_secret"`
}

func NewHandler(cfg config.WebhookConfig, database *db.DB, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{cfg: cfg, db: database, errorAdapter: httpapi.NewErrorAdapter(logger)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Post("/", wrap(h.create))
	router.Get("/", wrap(h.list))
	router.Get("/{webhook_id}", wrap(h.retrieveRoute))
	router.Post("/{webhook_id}/regenerate_signing_secret", wrap(h.regenerateSigningSecretRoute))
	router.Post("/{webhook_id}", wrap(h.updateRoute))
	router.Delete("/{webhook_id}", wrap(h.deleteRoute))
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !hasWebhooksBeta(r) {
		h.errorAdapter.Write(w, r, webhooksBetaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return webhookRouteNotFound()
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return webhookAuthenticationRequired()
	}
	fields, err := decodeWebhookObjectBody(w, r)
	if err != nil {
		return invalidRequest(err)
	}
	endpointURL, err := parseWebhookRequiredString(fields, "url")
	if err != nil {
		return invalidRequest(err)
	}
	if err := validateWebhookURL(endpointURL, h.cfg.AllowInsecure); err != nil {
		return invalidRequest(err)
	}
	name, err := parseWebhookRequiredString(fields, "name")
	if err != nil {
		return invalidRequest(err)
	}
	description := ""
	if raw, ok := fields["description"]; ok {
		description, err = parseWebhookRawString(raw, "description")
		if err != nil {
			return invalidRequest(err)
		}
	}
	enabledEvents, err := parseEnabledEvents(fields["enabled_events"])
	if err != nil {
		return invalidRequest(err)
	}
	webhookID, err := ids.New("wh_")
	if err != nil {
		return internalError("Could not create webhook", fmt.Errorf("generate webhook ID: %w", err))
	}
	signingSecret, err := newSigningSecret()
	if err != nil {
		return internalError("Could not create webhook", fmt.Errorf("generate webhook signing secret: %w", err))
	}
	now := time.Now().UTC()
	created, err := h.db.CreateWebhookEndpoint(r.Context(), db.WebhookEndpoint{
		UUID:                uuid.NewV4().String(),
		ExternalID:          webhookID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		URL:                 endpointURL,
		Name:                name,
		Description:         description,
		EnabledEvents:       enabledEvents,
		SigningSecret:       signingSecret,
		Status:              "enabled",
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		return internalError("Could not create webhook", fmt.Errorf("create webhook %q: %w", webhookID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWebhookEndpoint(created, true))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return webhookAuthenticationRequired()
	}
	records, err := h.db.ListWebhookEndpoints(r.Context(), principal.WorkspaceUUID)
	if err != nil {
		return internalError("Could not list webhooks", fmt.Errorf("list webhooks: %w", err))
	}
	data := make([]webhookResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromWebhookEndpoint(record, false))
	}
	httpapi.WriteJSON(w, http.StatusOK, webhookPageResponse{Data: data})
	return nil
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return webhookAuthenticationRequired()
	}
	webhookID := chi.URLParam(r, "webhook_id")
	record, err := h.db.GetWebhookEndpoint(r.Context(), principal.WorkspaceUUID, webhookID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return webhookNotFound(webhookID, err)
		}
		return internalError("Could not retrieve webhook", fmt.Errorf("retrieve webhook %q: %w", webhookID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWebhookEndpoint(record, false))
	return nil
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return webhookAuthenticationRequired()
	}
	webhookID := chi.URLParam(r, "webhook_id")
	current, err := h.db.GetWebhookEndpoint(r.Context(), principal.WorkspaceUUID, webhookID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return webhookNotFound(webhookID, err)
		}
		return internalError("Could not update webhook", fmt.Errorf("retrieve webhook %q for update: %w", webhookID, err))
	}
	fields, err := decodeWebhookObjectBody(w, r)
	if err != nil {
		return invalidRequest(err)
	}
	next := current
	if raw, ok := fields["url"]; ok {
		next.URL, err = parseWebhookRawString(raw, "url")
		if err != nil {
			return invalidRequest(err)
		}
	}
	if err := validateWebhookURL(next.URL, h.cfg.AllowInsecure); err != nil {
		return invalidRequest(err)
	}
	if raw, ok := fields["name"]; ok {
		next.Name, err = parseWebhookRawString(raw, "name")
		if err != nil {
			return invalidRequest(err)
		}
	}
	if raw, ok := fields["description"]; ok {
		next.Description, err = parseWebhookRawString(raw, "description")
		if err != nil {
			return invalidRequest(err)
		}
	}
	if raw, ok := fields["enabled_events"]; ok {
		next.EnabledEvents, err = parseEnabledEvents(raw)
		if err != nil {
			return invalidRequest(err)
		}
	}
	if raw, ok := fields["status"]; ok {
		next.Status, err = parseWebhookRawString(raw, "status")
		if err != nil {
			return invalidRequest(err)
		}
		switch next.Status {
		case "enabled":
			next.DisabledReason = nil
			next.ConsecutiveFailures = 0
		case "disabled":
			if next.DisabledReason == nil {
				reason := "manual"
				next.DisabledReason = &reason
			}
		default:
			return invalidRequest(errors.New("status must be enabled or disabled"))
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateWebhookEndpoint(r.Context(), principal.WorkspaceUUID, webhookID, next)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return webhookNotFound(webhookID, err)
		}
		return internalError("Could not update webhook", fmt.Errorf("update webhook %q: %w", webhookID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWebhookEndpoint(updated, false))
	return nil
}

func (h *Handler) regenerateSigningSecretRoute(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return webhookAuthenticationRequired()
	}
	if err := decodeWebhookEmptyObjectBody(w, r); err != nil {
		return invalidRequest(err)
	}
	signingSecret, err := newSigningSecret()
	if err != nil {
		return internalError("Could not regenerate webhook signing secret", fmt.Errorf("generate webhook signing secret: %w", err))
	}
	webhookID := chi.URLParam(r, "webhook_id")
	if err := h.db.RegenerateWebhookEndpointSigningSecret(r.Context(), principal.WorkspaceUUID, webhookID, signingSecret, time.Now().UTC()); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return webhookNotFound(webhookID, err)
		}
		return internalError("Could not regenerate webhook signing secret", fmt.Errorf("regenerate webhook %q signing secret: %w", webhookID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, regenerateSigningSecretResponse{SigningSecret: signingSecret})
	return nil
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return webhookAuthenticationRequired()
	}
	webhookID := chi.URLParam(r, "webhook_id")
	if err := h.db.DeleteWebhookEndpoint(r.Context(), principal.WorkspaceUUID, webhookID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return webhookNotFound(webhookID, err)
		}
		return internalError("Could not delete webhook", fmt.Errorf("delete webhook %q: %w", webhookID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: webhookID, Type: "webhook_deleted"})
	return nil
}

func responseFromWebhookEndpoint(record db.WebhookEndpoint, includeSecret bool) webhookResponse {
	response := webhookResponse{
		ID:             record.ExternalID,
		Type:           "webhook",
		URL:            record.URL,
		Name:           record.Name,
		Description:    record.Description,
		EnabledEvents:  append([]string(nil), record.EnabledEvents...),
		Status:         record.Status,
		DisabledReason: record.DisabledReason,
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if includeSecret {
		secret := record.SigningSecret
		response.SigningSecret = &secret
	}
	return response
}

func decodeWebhookEmptyObjectBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errors.New("Request body must be a JSON object")
	}
	if fields == nil {
		return errors.New("Request body must be a JSON object")
	}
	if len(fields) > 0 {
		return errors.New("Request body must be an empty JSON object")
	}
	return nil
}

func decodeWebhookObjectBody(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return nil, errors.New("Request body must be a JSON object")
	}
	if fields == nil {
		return nil, errors.New("Request body must be a JSON object")
	}
	return fields, nil
}

func parseWebhookRequiredString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return parseWebhookRawString(raw, name)
}

func parseWebhookRawString(raw json.RawMessage, name string) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	value = strings.TrimSpace(value)
	if value == "" && name != "description" {
		return "", fmt.Errorf("%s is required", name)
	}
	switch name {
	case "url":
		if len(value) > 2048 {
			return "", errors.New("url must be at most 2048 characters")
		}
	case "name":
		if len(value) > 255 {
			return "", errors.New("name must be at most 255 characters")
		}
	case "description":
		if len(value) > 2048 {
			return "", errors.New("description must be at most 2048 characters")
		}
	}
	return value, nil
}

func parseEnabledEvents(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("enabled_events is required")
	}
	var events []string
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, errors.New("enabled_events must be an array of strings")
	}
	if len(events) == 0 {
		return nil, errors.New("enabled_events must contain at least one event type")
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(events))
	for _, eventType := range events {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			return nil, errors.New("enabled_events must contain non-empty strings")
		}
		if _, ok := supportedEndpointEventTypes[eventType]; !ok {
			return nil, fmt.Errorf("unsupported enabled_events value: %s", eventType)
		}
		if _, ok := seen[eventType]; ok {
			return nil, fmt.Errorf("duplicate enabled_events value: %s", eventType)
		}
		seen[eventType] = struct{}{}
		result = append(result, eventType)
	}
	return result, nil
}

func validateWebhookURL(rawURL string, allowInsecure bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("url must be a valid URL")
	}
	if parsed.Scheme != "https" && !allowInsecure {
		return errors.New("url must use https unless webhook.allow_insecure is true")
	}
	if parsed.User != nil {
		return errors.New("url must not include credentials")
	}
	if parsed.Port() != "" && parsed.Port() != "443" && !allowInsecure {
		return errors.New("url must use port 443 unless webhook.allow_insecure is true")
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("url must include a host")
	}
	if isPrivateWebhookHost(host) && !allowInsecure {
		return errors.New("url host must be publicly routable unless webhook.allow_insecure is true")
	}
	return nil
}

func isPrivateWebhookHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	return false
}

func newSigningSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(secret), nil
}

func hasWebhooksBeta(r *http.Request) bool {
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == webhooksBeta {
				return true
			}
		}
	}
	return false
}
