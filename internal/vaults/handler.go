package vaults

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
)

const maxVaultBodySize = 4 << 20

type Handler struct {
	cfg          config.Config
	db           *db.DB
	secretSvc    *secrets.Service
	webhooks     webhookEnqueuer
	logger       *slog.Logger
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

type webhookEnqueuer interface {
	Enqueue(context.Context, webhooks.EnqueueInput)
}

type createVaultRequest struct {
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata"`
}

type updateVaultRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
	Metadata    json.RawMessage `json:"metadata"`
}

type createCredentialRequest struct {
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata"`
	Auth        json.RawMessage   `json:"auth"`
}

type updateCredentialRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
	Metadata    json.RawMessage `json:"metadata"`
	Auth        json.RawMessage `json:"auth"`
}

type pageCursorPayload struct {
	CreatedAt string `json:"created_at"`
	UUID      string `json:"uuid"`
}

type vaultResponse struct {
	ID          string          `json:"id"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedAt   string          `json:"created_at"`
	DisplayName string          `json:"display_name"`
	Metadata    json.RawMessage `json:"metadata"`
	Type        string          `json:"type"`
	UpdatedAt   string          `json:"updated_at"`
}

type vaultPageResponse struct {
	Data     []vaultResponse `json:"data"`
	NextPage *string         `json:"next_page"`
}

type credentialResponse struct {
	ID          string          `json:"id"`
	ArchivedAt  *string         `json:"archived_at"`
	Auth        credentialAuth  `json:"auth"`
	CreatedAt   string          `json:"created_at"`
	Metadata    json.RawMessage `json:"metadata"`
	Type        string          `json:"type"`
	UpdatedAt   string          `json:"updated_at"`
	VaultID     string          `json:"vault_id"`
	DisplayName string          `json:"display_name"`
}

type credentialPageResponse struct {
	Data     []credentialResponse `json:"data"`
	NextPage *string              `json:"next_page"`
}

type deleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type validationResponse struct {
	CredentialID    string            `json:"credential_id"`
	HasRefreshToken bool              `json:"has_refresh_token"`
	MCPProbe        *validationProbe  `json:"mcp_probe"`
	Refresh         validationRefresh `json:"refresh"`
	Status          string            `json:"status"`
	Type            string            `json:"type"`
	ValidatedAt     string            `json:"validated_at"`
	VaultID         string            `json:"vault_id"`
}

type validationProbe struct {
	HTTPResponse *validationHTTPResponse `json:"http_response"`
	Method       string                  `json:"method"`
}

type validationRefresh struct {
	HTTPResponse *validationHTTPResponse `json:"http_response"`
	Status       string                  `json:"status"`
}

type validationHTTPResponse struct {
	Body          string `json:"body"`
	BodyTruncated bool   `json:"body_truncated"`
	ContentType   string `json:"content_type"`
	StatusCode    int    `json:"status_code"`
}

func NewHandler(cfg config.Config, database *db.DB, secretSvc *secrets.Service, webhookEvents webhookEnqueuer, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{
		cfg:          cfg,
		db:           database,
		secretSvc:    secretSvc,
		webhooks:     webhookEvents,
		logger:       logger,
		errorAdapter: httpapi.NewErrorAdapter(logger),
	}
	router := chi.NewRouter()
	wrap := h.errorAdapter.Wrap
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Post("/", wrap(h.createVault))
	router.Get("/", wrap(h.listVaults))
	router.Route("/{vault_id}", func(r chi.Router) {
		r.Get("/", wrap(h.retrieveVaultRoute))
		r.Post("/", wrap(h.updateVaultRoute))
		r.Post("/archive", wrap(h.archiveVaultRoute))
		r.Delete("/", wrap(h.deleteVaultRoute))
		r.Route("/credentials", func(r chi.Router) {
			r.Post("/", wrap(h.createCredentialRoute))
			r.Get("/", wrap(h.listCredentialsRoute))
			r.Get("/{credential_id}", wrap(h.retrieveCredentialRoute))
			r.Post("/{credential_id}", wrap(h.updateCredentialRoute))
			r.Post("/{credential_id}/archive", wrap(h.archiveCredentialRoute))
			r.Delete("/{credential_id}", wrap(h.deleteCredentialRoute))
			r.Post("/{credential_id}/mcp_oauth_validate", wrap(h.validateCredentialRoute))
		})
	})
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		h.errorAdapter.Write(w, r, vaultBetaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return routeNotFound()
}

func (h *Handler) createVault(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	request, err := httpapi.DecodeObjectBodyAs[createVaultRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	displayName, err := requireNonEmptyString(request.DisplayName, "display_name")
	if err == nil {
		err = validateDisplayName(displayName)
	}
	if err != nil {
		return invalidRequest(err)
	}
	metadata, err := normalizeMetadata(request.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	vaultID, err := ids.New("vlt_")
	if err != nil {
		return internalError("Could not generate vault ID", fmt.Errorf("generate vault ID: %w", err))
	}
	now := time.Now().UTC()
	created, err := h.db.CreateVault(r.Context(), db.Vault{
		UUID:                uuid.NewV4().String(),
		ExternalID:          vaultID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		DisplayName:         displayName,
		Metadata:            metadata,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		return internalError("Could not create vault", fmt.Errorf("create vault: %w", err))
	}
	h.enqueueWebhook(r, principal, "vault.created", created.ExternalID, nil)
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(created))
	return nil
}

func (h *Handler) listVaults(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	limit, err := parseLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeVaultCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListVaultsPage(r.Context(), db.ListVaultsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		return internalError("Could not list vaults", fmt.Errorf("list vaults: %w", err))
	}
	data := make([]vaultResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromVault(record))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeVaultCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, vaultPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveVaultRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	record, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not retrieve vault", fmt.Errorf("retrieve vault %q: %w", vaultID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(record))
	return nil
}

func (h *Handler) updateVaultRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	current, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not update vault", fmt.Errorf("get vault %q before update: %w", vaultID, err))
	}
	if current.ArchivedAt != nil {
		return vaultArchived()
	}
	request, err := httpapi.DecodeObjectBodyAs[updateVaultRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	next := current
	next.DisplayName, err = patchDisplayName(next.DisplayName, request.DisplayName)
	if err != nil {
		return invalidRequest(err)
	}
	next.Metadata, err = patchMetadata(next.Metadata, request.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateVault(r.Context(), principal.WorkspaceUUID, vaultID, next)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not update vault", fmt.Errorf("update vault %q: %w", vaultID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(updated))
	return nil
}

func (h *Handler) archiveVaultRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentials := h.loadVaultCredentialsForWebhook(r, principal.WorkspaceUUID, vaultID, false)
	record, err := h.db.ArchiveVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not archive vault", fmt.Errorf("archive vault %q: %w", vaultID, err))
	}
	h.enqueueWebhook(r, principal, "vault.archived", record.ExternalID, nil)
	for _, credential := range credentials {
		parentVaultID := record.ExternalID
		h.enqueueWebhookWithOptions(r, principal, "vault_credential.archived", credential.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(record))
	return nil
}

func (h *Handler) deleteVaultRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentials := h.loadVaultCredentialsForWebhook(r, principal.WorkspaceUUID, vaultID, true)
	if err := h.db.DeleteVault(r.Context(), principal.WorkspaceUUID, vaultID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not delete vault", fmt.Errorf("delete vault %q: %w", vaultID, err))
	}
	h.enqueueWebhook(r, principal, "vault.deleted", vaultID, nil)
	for _, credential := range credentials {
		parentVaultID := vaultID
		h.enqueueWebhookWithOptions(r, principal, "vault_credential.deleted", credential.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: vaultID, Type: "vault_deleted"})
	return nil
}

func (h *Handler) createCredentialRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	vault, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not create credential", fmt.Errorf("get vault %q before credential create: %w", vaultID, err))
	}
	if vault.ArchivedAt != nil {
		return vaultArchived()
	}
	request, err := httpapi.DecodeObjectBodyAs[createCredentialRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	displayName, err := requireNonEmptyString(request.DisplayName, "display_name")
	if err == nil {
		err = validateDisplayName(displayName)
	}
	if err != nil {
		return invalidRequest(err)
	}
	metadata, err := normalizeMetadata(request.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	authState, err := normalizeCredentialAuthForCreate(request.Auth)
	if err != nil {
		return invalidRequest(err)
	}
	credentialID, err := ids.New("vcrd_")
	if err != nil {
		return internalError("Could not generate credential ID", fmt.Errorf("generate credential ID: %w", err))
	}
	now := time.Now().UTC()
	credential := db.VaultCredential{
		UUID:                uuid.NewV4().String(),
		ExternalID:          credentialID,
		OrganizationUUID:    vault.OrganizationUUID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		VaultUUID:           vault.UUID,
		VaultExternalID:     vault.ExternalID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		DisplayName:         displayName,
		Metadata:            metadata,
		AuthType:            authState.AuthType,
		CredentialKey:       authState.Key,
		Auth:                authState.PublicAuth,
		SecretPayload:       authState.SecretPayload,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := SealCredentialSecret(r.Context(), h.secretSvc, &credential); err != nil {
		return internalError("Could not create credential", fmt.Errorf("seal vault credential: %w", err))
	}
	created, err := h.db.CreateVaultCredential(r.Context(), credential)
	if err != nil {
		return mapCreateCredentialError(err, vaultID)
	}
	parentVaultID := created.VaultExternalID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.created", created.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	return h.writeCredentialResponse(w, created)
}

func (h *Handler) listCredentialsRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	if _, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return vaultNotFound(vaultID, err)
		}
		return internalError("Could not list credentials", fmt.Errorf("get vault %q before credential list: %w", vaultID, err))
	}
	limit, err := parseLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeCredentialCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListVaultCredentialsPage(r.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		VaultExternalID: vaultID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		return internalError("Could not list credentials", fmt.Errorf("list vault credentials: %w", err))
	}
	data, err := responsesFromCredentials(records)
	if err != nil {
		return internalError("Could not list credentials", fmt.Errorf("decode vault credential auth: %w", err))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeCredentialCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, credentialPageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveCredentialRoute(w http.ResponseWriter, r *http.Request) error {
	credential, err := h.authorizeCredential(r)
	if err != nil {
		return err
	}
	return h.writeCredentialResponse(w, credential)
}

func (h *Handler) updateCredentialRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	current, err := h.db.GetVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return credentialNotFound(credentialID, err)
		}
		return internalError("Could not update credential", fmt.Errorf("get credential %q before update: %w", credentialID, err))
	}
	if current.ArchivedAt != nil {
		return credentialArchived()
	}
	request, err := httpapi.DecodeObjectBodyAs[updateCredentialRequest](w, r, maxVaultBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	next := current
	next.DisplayName, err = patchDisplayName(next.DisplayName, request.DisplayName)
	if err != nil {
		return invalidRequest(err)
	}
	next.Metadata, err = patchMetadata(next.Metadata, request.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	if len(request.Auth) != 0 {
		// Open the existing envelope so partial updates (for example rotating
		// only an OAuth access token) merge onto stored refresh material
		// instead of dropping it, then reseal under a fresh DEK.
		// Without an envelope, auth normalization requires a complete
		// replacement secret before allowing the credential to be resealed.
		var currentSecret []byte
		if current.SecretEnvelope != nil {
			currentSecret, err = openCredentialSecret(r.Context(), h.secretSvc, current)
			if err != nil {
				return credentialSecretError(err, "Could not update credential")
			}
			defer clear(currentSecret)
		}
		authState, err := normalizeCredentialAuthForUpdate(current, currentSecret, request.Auth)
		if err != nil {
			return invalidRequest(err)
		}
		next.Auth = authState.PublicAuth
		next.SecretPayload = authState.SecretPayload
		next.CredentialKey = authState.Key
		if err := SealCredentialSecret(r.Context(), h.secretSvc, &next); err != nil {
			return internalError("Could not update credential", fmt.Errorf("seal vault credential: %w", err))
		}
	} else if current.SecretEnvelope == nil {
		return credentialSecretError(ErrMissingSecretEnvelope, "Could not update credential")
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID, next)
	if err != nil {
		return mapUpdateCredentialError(err, credentialID)
	}
	return h.writeCredentialResponse(w, updated)
}

func (h *Handler) archiveCredentialRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	record, err := h.db.ArchiveVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return credentialNotFound(credentialID, err)
		}
		return internalError("Could not archive credential", fmt.Errorf("archive credential %q: %w", credentialID, err))
	}
	parentVaultID := record.VaultExternalID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.archived", record.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	return h.writeCredentialResponse(w, record)
}

func (h *Handler) deleteCredentialRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	if err := h.db.DeleteVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return credentialNotFound(credentialID, err)
		}
		return internalError("Could not delete credential", fmt.Errorf("delete credential %q: %w", credentialID, err))
	}
	parentVaultID := vaultID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.deleted", credentialID, webhooks.EventOptions{VaultID: &parentVaultID})
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: credentialID, Type: "vault_credential_deleted"})
	return nil
}

func (h *Handler) enqueueWebhook(r *http.Request, principal auth.Principal, eventType, resourceID string, sessionThreadID *string) {
	h.enqueueWebhookWithOptions(r, principal, eventType, resourceID, webhooks.EventOptions{SessionThreadID: sessionThreadID})
}

func (h *Handler) enqueueWebhookWithOptions(r *http.Request, principal auth.Principal, eventType, resourceID string, options webhooks.EventOptions) {
	if h.webhooks == nil {
		return
	}
	h.webhooks.Enqueue(r.Context(), webhooks.EnqueueInput{
		WorkspaceUUID:       principal.WorkspaceUUID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceExternalID: principal.WorkspaceExternalID,
		EventType:           eventType,
		ResourceID:          resourceID,
		Options:             options,
	})
}

func (h *Handler) loadVaultCredentialsForWebhook(r *http.Request, workspaceUUID, vaultID string, includeArchived bool) []db.VaultCredential {
	records, _, err := h.db.ListVaultCredentialsPage(r.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceUUID:   workspaceUUID,
		VaultExternalID: vaultID,
		Limit:           1000,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list vault credentials for webhook", "vault_id", vaultID, "error", err)
		return nil
	}
	return records
}

func (h *Handler) validateCredentialRoute(w http.ResponseWriter, r *http.Request) error {
	credential, err := h.authorizeCredential(r)
	if err != nil {
		return err
	}
	if credential.AuthType != "mcp_oauth" {
		return credentialRequiresMCPOAuth()
	}
	// Decrypt transiently to inspect whether a refresh token is present. The
	// plaintext is not persisted or logged.
	plaintext, err := openCredentialSecret(r.Context(), h.secretSvc, credential)
	if err != nil {
		return credentialSecretError(err, "Could not validate credential")
	}
	defer clear(plaintext)
	secret, err := decodeMCPOAuthCredentialSecret(plaintext)
	if err != nil {
		return credentialSecretError(err, "Could not validate credential")
	}
	hasRefreshToken := secret.Refresh != nil && secret.Refresh.RefreshToken != ""
	refreshStatus := "no_refresh_token"
	if hasRefreshToken {
		refreshStatus = "connect_error"
	}
	httpapi.WriteJSON(w, http.StatusOK, validationResponse{
		CredentialID:    credential.ExternalID,
		HasRefreshToken: hasRefreshToken,
		MCPProbe:        nil,
		Refresh: validationRefresh{
			HTTPResponse: nil,
			Status:       refreshStatus,
		},
		Status:      "unknown",
		Type:        "vault_credential_validation",
		ValidatedAt: formatTime(time.Now().UTC()),
		VaultID:     credential.VaultExternalID,
	})
	return nil
}

func (h *Handler) authorizeCredential(r *http.Request) (db.VaultCredential, error) {
	principal, err := requireAPIKey(r)
	if err != nil {
		return db.VaultCredential{}, err
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	credential, err := h.db.GetVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.VaultCredential{}, credentialNotFound(credentialID, err)
		}
		return db.VaultCredential{}, internalError("Could not retrieve credential", fmt.Errorf("retrieve credential %q: %w", credentialID, err))
	}
	return credential, nil
}

func requireAPIKey(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !isWorkspaceCredential(principal) {
		return auth.Principal{}, missingAPIKey()
	}
	return principal, nil
}

func isWorkspaceCredential(principal auth.Principal) bool {
	return principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession
}

func responseFromVault(vault db.Vault) vaultResponse {
	return vaultResponse{
		ID:          vault.ExternalID,
		ArchivedAt:  optionalTime(vault.ArchivedAt),
		CreatedAt:   formatTime(vault.CreatedAt),
		DisplayName: vault.DisplayName,
		Metadata:    rawOr(vault.Metadata, `{}`),
		Type:        "vault",
		UpdatedAt:   formatTime(vault.UpdatedAt),
	}
}

func responseFromCredential(credential db.VaultCredential) (credentialResponse, error) {
	auth, err := decodeCredentialAuth(credential.Auth)
	if err != nil {
		return credentialResponse{}, fmt.Errorf("decode credential %s auth: %w", credential.ExternalID, err)
	}
	return credentialResponse{
		ID:          credential.ExternalID,
		ArchivedAt:  optionalTime(credential.ArchivedAt),
		Auth:        auth,
		CreatedAt:   formatTime(credential.CreatedAt),
		Metadata:    rawOr(credential.Metadata, `{}`),
		Type:        "vault_credential",
		UpdatedAt:   formatTime(credential.UpdatedAt),
		VaultID:     credential.VaultExternalID,
		DisplayName: credential.DisplayName,
	}, nil
}

func responsesFromCredentials(credentials []db.VaultCredential) ([]credentialResponse, error) {
	responses := make([]credentialResponse, 0, len(credentials))
	for _, credential := range credentials {
		response, err := responseFromCredential(credential)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (h *Handler) writeCredentialResponse(w http.ResponseWriter, credential db.VaultCredential) error {
	response, err := responseFromCredential(credential)
	if err != nil {
		return internalError("Could not encode credential", fmt.Errorf("decode credential %q auth: %w", credential.ExternalID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func normalizeMetadata(metadata map[string]string) (json.RawMessage, error) {
	if metadata == nil {
		return json.RawMessage(`{}`), nil
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return json.Marshal(metadata)
}

func patchDisplayName(current string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return current, nil
	}
	if isJSONNull(raw) {
		return "", errors.New("display_name cannot be null")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("display_name must be a string")
	}
	displayName, err := requireNonEmptyString(value, "display_name")
	if err != nil {
		return "", err
	}
	if err := validateDisplayName(displayName); err != nil {
		return "", err
	}
	return displayName, nil
}

func patchMetadata(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return current, nil
	}
	if isJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var patch map[string]*string
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	var metadata map[string]string
	if len(current) == 0 || isJSONNull(current) {
		metadata = map[string]string{}
	} else if err := json.Unmarshal(current, &metadata); err != nil {
		return nil, errors.New("existing metadata is invalid")
	}
	for key, value := range patch {
		if value == nil || *value == "" {
			delete(metadata, key)
			continue
		}
		metadata[key] = *value
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return json.Marshal(metadata)
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > 16 {
		return errors.New("metadata may contain at most 16 entries")
	}
	for key, value := range metadata {
		if key == "" || len(key) > 64 {
			return errors.New("metadata keys must be between 1 and 64 characters")
		}
		if len(value) > 512 {
			return errors.New("metadata values must be at most 512 characters")
		}
	}
	return nil
}

func validateDisplayName(displayName string) error {
	if len(displayName) > 255 {
		return errors.New("display_name must be at most 255 characters")
	}
	return nil
}

func parseLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, errors.New("limit must be between 1 and 100")
	}
	return limit, nil
}

func parseOptionalBool(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	switch strings.ToLower(raw) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", name)
	}
}

func encodeVaultCursor(vault db.Vault) string {
	data, _ := json.Marshal(pageCursorPayload{CreatedAt: formatTime(vault.CreatedAt), UUID: vault.UUID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeVaultCursor(raw string) (*db.VaultPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var payload pageCursorPayload
	if err := json.Unmarshal(data, &payload); err != nil || payload.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	if _, err := uuid.Parse(payload.UUID); err != nil {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.VaultPageCursor{CreatedAt: createdAt.UTC(), UUID: payload.UUID}, nil
}

func encodeCredentialCursor(credential db.VaultCredential) string {
	data, _ := json.Marshal(pageCursorPayload{CreatedAt: formatTime(credential.CreatedAt), UUID: credential.UUID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCredentialCursor(raw string) (*db.VaultCredentialPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var payload pageCursorPayload
	if err := json.Unmarshal(data, &payload); err != nil || payload.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	if _, err := uuid.Parse(payload.UUID); err != nil {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.VaultCredentialPageCursor{CreatedAt: createdAt.UTC(), UUID: payload.UUID}, nil
}

func rawOr(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func optionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
