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

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxVaultBodySize = 4 << 20

type Handler struct {
	cfg       config.Config
	db        *db.DB
	secretSvc *secrets.Service
	webhooks  webhookEnqueuer
	logger    *slog.Logger
	router    chi.Router
}

type webhookEnqueuer interface {
	Enqueue(context.Context, webhooks.EnqueueInput)
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

func writeSecretOpenError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, operation string, err error) {
	if errors.Is(err, ErrMissingSecretEnvelope) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", ErrMissingSecretEnvelope.Error()))
		return
	}
	logger.ErrorContext(r.Context(), "open vault credential", "operation", operation, "error", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not "+operation+" credential"))
}

func NewHandler(cfg config.Config, database *db.DB, secretSvc *secrets.Service, webhookEvents webhookEnqueuer, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{
		cfg:       cfg,
		db:        database,
		secretSvc: secretSvc,
		webhooks:  webhookEvents,
		logger:    logger,
	}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.createVault)
	router.Get("/", h.listVaults)
	router.Route("/{vault_id}", func(r chi.Router) {
		r.Get("/", h.retrieveVaultRoute)
		r.Post("/", h.updateVaultRoute)
		r.Post("/archive", h.archiveVaultRoute)
		r.Delete("/", h.deleteVaultRoute)
		r.Route("/credentials", func(r chi.Router) {
			r.Post("/", h.createCredentialRoute)
			r.Get("/", h.listCredentialsRoute)
			r.Get("/{credential_id}", h.retrieveCredentialRoute)
			r.Post("/{credential_id}", h.updateCredentialRoute)
			r.Post("/{credential_id}/archive", h.archiveCredentialRoute)
			r.Delete("/{credential_id}", h.deleteCredentialRoute)
			r.Post("/{credential_id}/mcp_oauth_validate", h.validateCredentialRoute)
		})
	})
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Vaults API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) createVault(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	displayName, err := requiredString(fields, "display_name", "display_name")
	if err == nil {
		err = validateDisplayName(displayName)
	}
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	metadata, err := normalizeMetadata(fieldOrDefault(fields, "metadata", `{}`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	vaultID, err := ids.New("vlt_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate vault ID"))
		return
	}
	now := time.Now().UTC()
	created, err := h.db.CreateVault(r.Context(), db.Vault{
		UUID:                uuid.NewString(),
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
		h.logger.ErrorContext(r.Context(), "create vault", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create vault"))
		return
	}
	h.enqueueWebhook(r, principal, "vault.created", created.ExternalID, nil)
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(created))
}

func (h *Handler) listVaults(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeVaultCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListVaultsPage(r.Context(), db.ListVaultsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list vaults", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list vaults"))
		return
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
}

func (h *Handler) retrieveVaultRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	record, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "get vault", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve vault"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(record))
}

func (h *Handler) updateVaultRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	current, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "get vault before update", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update vault"))
		return
	}
	if current.ArchivedAt != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Vault is archived"))
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	next := current
	if raw, ok := fields["display_name"]; ok {
		displayName, err := rawString(raw, "display_name")
		if err == nil {
			err = validateDisplayName(displayName)
		}
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.DisplayName = displayName
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = patchMetadata(next.Metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateVault(r.Context(), principal.WorkspaceUUID, vaultID, next)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "update vault", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update vault"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(updated))
}

func (h *Handler) archiveVaultRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentials := h.loadVaultCredentialsForWebhook(r, principal.WorkspaceUUID, vaultID, false)
	record, err := h.db.ArchiveVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "archive vault", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not archive vault"))
		return
	}
	h.enqueueWebhook(r, principal, "vault.archived", record.ExternalID, nil)
	for _, credential := range credentials {
		parentVaultID := record.ExternalID
		h.enqueueWebhookWithOptions(r, principal, "vault_credential.archived", credential.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromVault(record))
}

func (h *Handler) deleteVaultRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentials := h.loadVaultCredentialsForWebhook(r, principal.WorkspaceUUID, vaultID, true)
	if err := h.db.DeleteVault(r.Context(), principal.WorkspaceUUID, vaultID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "delete vault", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete vault"))
		return
	}
	h.enqueueWebhook(r, principal, "vault.deleted", vaultID, nil)
	for _, credential := range credentials {
		parentVaultID := vaultID
		h.enqueueWebhookWithOptions(r, principal, "vault_credential.deleted", credential.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: vaultID, Type: "vault_deleted"})
}

func (h *Handler) createCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	vault, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "get vault before credential create", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create credential"))
		return
	}
	if vault.ArchivedAt != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Vault is archived"))
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	displayName, err := requiredString(fields, "display_name", "display_name")
	if err == nil {
		err = validateDisplayName(displayName)
	}
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	metadata, err := normalizeMetadata(fieldOrDefault(fields, "metadata", `{}`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	authState, err := normalizeCredentialAuthForCreate(fields["auth"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	credentialID, err := ids.New("vcrd_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate credential ID"))
		return
	}
	now := time.Now().UTC()
	credential := db.VaultCredential{
		UUID:                uuid.NewString(),
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
		h.logger.ErrorContext(r.Context(), "seal vault credential", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create credential"))
		return
	}
	created, err := h.db.CreateVaultCredential(r.Context(), credential)
	if err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Credential key already exists"))
			return
		}
		if errors.Is(err, db.ErrLimitExceeded) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Vault may contain at most 20 active credentials"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "create vault credential", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create credential"))
		return
	}
	parentVaultID := created.VaultExternalID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.created", created.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	h.writeCredentialResponse(w, r, created)
}

func (h *Handler) listCredentialsRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	if _, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		h.logger.ErrorContext(r.Context(), "get vault before credential list", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list credentials"))
		return
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeCredentialCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListVaultCredentialsPage(r.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		VaultExternalID: vaultID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list vault credentials", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list credentials"))
		return
	}
	data, err := responsesFromCredentials(records)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "decode vault credential auth", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list credentials"))
		return
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeCredentialCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, credentialPageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveCredentialRoute(w http.ResponseWriter, r *http.Request) {
	credential, ok := h.authorizeCredential(w, r, "retrieve")
	if !ok {
		return
	}
	h.writeCredentialResponse(w, r, credential)
}

func (h *Handler) updateCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	current, err := h.db.GetVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		h.logger.ErrorContext(r.Context(), "get credential before update", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update credential"))
		return
	}
	if current.ArchivedAt != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Credential is archived"))
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	next := current
	if raw, ok := fields["display_name"]; ok {
		displayName, err := rawString(raw, "display_name")
		if err == nil {
			err = validateDisplayName(displayName)
		}
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.DisplayName = displayName
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = patchMetadata(next.Metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["auth"]; ok {
		// Open the existing envelope so partial updates (for example rotating
		// only an OAuth access token) merge onto stored refresh material
		// instead of dropping it, then reseal under a fresh DEK.
		// A missing envelope can still be repaired when the body carries a
		// complete replacement secret; otherwise ask the client to resubmit.
		var currentSecret []byte
		if current.SecretEnvelope != nil {
			currentSecret, err = openCredentialSecret(r.Context(), h.secretSvc, current)
			if err != nil {
				writeSecretOpenError(w, r, h.logger, "update", err)
				return
			}
			defer clear(currentSecret)
		} else {
			provides, err := authUpdateProvidesSecretReplacement(current.AuthType, raw)
			if err != nil {
				writeBadRequest(w, r, err)
				return
			}
			if !provides {
				writeSecretOpenError(w, r, h.logger, "update", ErrMissingSecretEnvelope)
				return
			}
		}
		authState, err := normalizeCredentialAuthForUpdate(current, currentSecret, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.Auth = authState.PublicAuth
		next.SecretPayload = authState.SecretPayload
		next.CredentialKey = authState.Key
		if err := SealCredentialSecret(r.Context(), h.secretSvc, &next); err != nil {
			h.logger.ErrorContext(r.Context(), "seal vault credential", "error", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update credential"))
			return
		}
	} else if current.SecretEnvelope == nil {
		writeSecretOpenError(w, r, h.logger, "update", ErrMissingSecretEnvelope)
		return
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID, next)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		if errors.Is(err, db.ErrDuplicate) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Credential key already exists"))
			return
		}
		if errors.Is(err, db.ErrVersionConflict) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Credential was modified concurrently; reload and try again"))
			return
		}
		h.logger.ErrorContext(r.Context(), "update vault credential", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update credential"))
		return
	}
	h.writeCredentialResponse(w, r, updated)
}

func (h *Handler) archiveCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	record, err := h.db.ArchiveVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		h.logger.ErrorContext(r.Context(), "archive vault credential", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not archive credential"))
		return
	}
	parentVaultID := record.VaultExternalID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.archived", record.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	h.writeCredentialResponse(w, r, record)
}

func (h *Handler) deleteCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	if err := h.db.DeleteVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		h.logger.ErrorContext(r.Context(), "delete vault credential", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete credential"))
		return
	}
	parentVaultID := vaultID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.deleted", credentialID, webhooks.EventOptions{VaultID: &parentVaultID})
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: credentialID, Type: "vault_credential_deleted"})
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

func (h *Handler) validateCredentialRoute(w http.ResponseWriter, r *http.Request) {
	credential, ok := h.authorizeCredential(w, r, "validate")
	if !ok {
		return
	}
	if credential.AuthType != "mcp_oauth" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "mcp_oauth_validate requires an mcp_oauth credential"))
		return
	}
	// Decrypt transiently to inspect whether a refresh token is present. The
	// plaintext is not persisted or logged.
	plaintext, err := openCredentialSecret(r.Context(), h.secretSvc, credential)
	if err != nil {
		writeSecretOpenError(w, r, h.logger, "validate", err)
		return
	}
	defer clear(plaintext)
	secret, err := decodeMCPOAuthCredentialSecret(plaintext)
	if err != nil {
		writeSecretOpenError(w, r, h.logger, "validate", err)
		return
	}
	hasRefreshToken := secret.Refresh != nil && strings.TrimSpace(secret.Refresh.RefreshToken) != ""
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
}

func (h *Handler) authorizeCredential(w http.ResponseWriter, r *http.Request, operation string) (db.VaultCredential, bool) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return db.VaultCredential{}, false
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	credential, err := h.db.GetVaultCredential(r.Context(), principal.WorkspaceUUID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return db.VaultCredential{}, false
		}
		h.logger.ErrorContext(r.Context(), "vault credential", "operation", operation, "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve credential"))
		return db.VaultCredential{}, false
	}
	return credential, true
}

func requireAPIKey(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !isWorkspaceCredential(principal) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	return principal, true
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

func (h *Handler) writeCredentialResponse(w http.ResponseWriter, r *http.Request, credential db.VaultCredential) {
	response, err := responseFromCredential(credential)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "decode vault credential auth", "credential_id", credential.ExternalID, "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not encode credential"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func decodeObjectBody(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxVaultBodySize)
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil {
		return nil, errors.New("Invalid JSON body")
	}
	if fields == nil {
		return nil, errors.New("JSON body must be an object")
	}
	return fields, nil
}

func normalizeMetadata(raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, errors.New("metadata must be an object with string values")
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return marshalRaw(metadata)
}

func patchMetadata(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
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
	return marshalRaw(metadata)
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
	data, _ := json.Marshal(map[string]any{"created_at": formatTime(vault.CreatedAt), "uuid": vault.UUID})
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
	var payload struct {
		CreatedAt string `json:"created_at"`
		UUID      string `json:"uuid"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || uuid.Validate(payload.UUID) != nil || payload.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.VaultPageCursor{CreatedAt: createdAt.UTC(), UUID: payload.UUID}, nil
}

func encodeCredentialCursor(credential db.VaultCredential) string {
	data, _ := json.Marshal(map[string]any{"created_at": formatTime(credential.CreatedAt), "uuid": credential.UUID})
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
	var payload struct {
		CreatedAt string `json:"created_at"`
		UUID      string `json:"uuid"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || uuid.Validate(payload.UUID) != nil || payload.CreatedAt == "" {
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

func writeBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
}
