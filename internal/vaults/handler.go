package vaults

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxVaultBodySize = 4 << 20

var credentialHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9.-]+$`)

type Handler struct {
	cfg    config.Config
	db     *db.DB
	router chi.Router
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
	Auth        json.RawMessage `json:"auth"`
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

type credentialAuthState struct {
	AuthType      string
	Key           string
	PublicAuth    json.RawMessage
	SecretPayload json.RawMessage
}

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	h := &Handler{cfg: cfg, db: database}
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
	displayName, err := parseRequiredStringField(fields, "display_name")
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
		UUID:              uuid.NewString(),
		ExternalID:        vaultID,
		OrganizationID:    principal.OrganizationID,
		WorkspaceID:       principal.WorkspaceID,
		CreatedByAPIKeyID: principal.APIKeyID,
		DisplayName:       displayName,
		Metadata:          metadata,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		log.Printf("create vault: %v", err)
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
		WorkspaceID:     principal.WorkspaceID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		log.Printf("list vaults: %v", err)
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
	record, err := h.db.GetVault(r.Context(), principal.WorkspaceID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("get vault: %v", err)
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
	current, err := h.db.GetVault(r.Context(), principal.WorkspaceID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("get vault before update: %v", err)
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
		next.DisplayName, err = parseRequiredRawString(raw, "display_name")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = patchMetadata(next.Metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateVault(r.Context(), principal.WorkspaceID, vaultID, next)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("update vault: %v", err)
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
	credentials := h.loadVaultCredentialsForWebhook(r, principal.WorkspaceID, vaultID, false)
	record, err := h.db.ArchiveVault(r.Context(), principal.WorkspaceID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("archive vault: %v", err)
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
	credentials := h.loadVaultCredentialsForWebhook(r, principal.WorkspaceID, vaultID, true)
	if err := h.db.DeleteVault(r.Context(), principal.WorkspaceID, vaultID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("delete vault: %v", err)
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
	vault, err := h.db.GetVault(r.Context(), principal.WorkspaceID, vaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("get vault before credential create: %v", err)
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
	displayName, err := parseRequiredStringField(fields, "display_name")
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
	created, err := h.db.CreateVaultCredential(r.Context(), db.VaultCredential{
		UUID:              uuid.NewString(),
		ExternalID:        credentialID,
		OrganizationID:    vault.OrganizationID,
		WorkspaceID:       principal.WorkspaceID,
		VaultID:           vault.ID,
		VaultExternalID:   vault.ExternalID,
		CreatedByAPIKeyID: principal.APIKeyID,
		DisplayName:       displayName,
		Metadata:          metadata,
		AuthType:          authState.AuthType,
		CredentialKey:     authState.Key,
		Auth:              authState.PublicAuth,
		SecretPayload:     authState.SecretPayload,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
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
		log.Printf("create vault credential: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create credential"))
		return
	}
	parentVaultID := created.VaultExternalID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.created", created.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	httpapi.WriteJSON(w, http.StatusOK, responseFromCredential(created))
}

func (h *Handler) listCredentialsRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	if _, err := h.db.GetVault(r.Context(), principal.WorkspaceID, vaultID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Vault not found: "+vaultID))
			return
		}
		log.Printf("get vault before credential list: %v", err)
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
		WorkspaceID:     principal.WorkspaceID,
		VaultExternalID: vaultID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		log.Printf("list vault credentials: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list credentials"))
		return
	}
	data := make([]credentialResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromCredential(record))
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
	httpapi.WriteJSON(w, http.StatusOK, responseFromCredential(credential))
}

func (h *Handler) updateCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	current, err := h.db.GetVaultCredential(r.Context(), principal.WorkspaceID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		log.Printf("get credential before update: %v", err)
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
		next.DisplayName, err = parseRequiredRawString(raw, "display_name")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = patchMetadata(next.Metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["auth"]; ok {
		authState, err := normalizeCredentialAuthForUpdate(current, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.Auth = authState.PublicAuth
		next.SecretPayload = authState.SecretPayload
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateVaultCredential(r.Context(), principal.WorkspaceID, vaultID, credentialID, next)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		log.Printf("update vault credential: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update credential"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromCredential(updated))
}

func (h *Handler) archiveCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	record, err := h.db.ArchiveVaultCredential(r.Context(), principal.WorkspaceID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		log.Printf("archive vault credential: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not archive credential"))
		return
	}
	parentVaultID := record.VaultExternalID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.archived", record.ExternalID, webhooks.EventOptions{VaultID: &parentVaultID})
	httpapi.WriteJSON(w, http.StatusOK, responseFromCredential(record))
}

func (h *Handler) deleteCredentialRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	vaultID := chi.URLParam(r, "vault_id")
	credentialID := chi.URLParam(r, "credential_id")
	if err := h.db.DeleteVaultCredential(r.Context(), principal.WorkspaceID, vaultID, credentialID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return
		}
		log.Printf("delete vault credential: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete credential"))
		return
	}
	parentVaultID := vaultID
	h.enqueueWebhookWithOptions(r, principal, "vault_credential.deleted", credentialID, webhooks.EventOptions{VaultID: &parentVaultID})
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: credentialID, Type: "vault_credential_deleted"})
}

func (h *Handler) enqueueWebhook(r *http.Request, principal auth.Principal, eventType, resourceID string, sessionThreadID *string) {
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, eventType, resourceID, sessionThreadID)
}

func (h *Handler) enqueueWebhookWithOptions(r *http.Request, principal auth.Principal, eventType, resourceID string, options webhooks.EventOptions) {
	webhooks.EnqueueWithOptions(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, eventType, resourceID, options)
}

func (h *Handler) loadVaultCredentialsForWebhook(r *http.Request, workspaceID int64, vaultID string, includeArchived bool) []db.VaultCredential {
	records, _, err := h.db.ListVaultCredentialsPage(r.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceID:     workspaceID,
		VaultExternalID: vaultID,
		Limit:           1000,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		log.Printf("list vault credentials for webhook vault_id=%s: %v", vaultID, err)
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
	hasRefreshToken := hasNestedSecret(credential.SecretPayload, "refresh", "refresh_token")
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
	credential, err := h.db.GetVaultCredential(r.Context(), principal.WorkspaceID, vaultID, credentialID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Credential not found: "+credentialID))
			return db.VaultCredential{}, false
		}
		log.Printf("%s vault credential: %v", operation, err)
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

func responseFromCredential(credential db.VaultCredential) credentialResponse {
	return credentialResponse{
		ID:          credential.ExternalID,
		ArchivedAt:  optionalTime(credential.ArchivedAt),
		Auth:        rawOr(credential.Auth, `{}`),
		CreatedAt:   formatTime(credential.CreatedAt),
		Metadata:    rawOr(credential.Metadata, `{}`),
		Type:        "vault_credential",
		UpdatedAt:   formatTime(credential.UpdatedAt),
		VaultID:     credential.VaultExternalID,
		DisplayName: credential.DisplayName,
	}
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

func parseRequiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return parseRequiredRawString(raw, name)
}

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
	if isJSONNull(raw) {
		return "", fmt.Errorf("%s cannot be null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	if len(value) > 255 {
		return "", fmt.Errorf("%s must be at most 255 characters", name)
	}
	return value, nil
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

func normalizeCredentialAuthForCreate(raw json.RawMessage) (credentialAuthState, error) {
	fields, err := objectFromRaw(raw, "auth")
	if err != nil {
		return credentialAuthState{}, err
	}
	authType, err := requiredString(fields, "type", "auth.type")
	if err != nil {
		return credentialAuthState{}, err
	}
	switch authType {
	case "mcp_oauth":
		return normalizeMCPOAuthForCreate(fields)
	case "static_bearer":
		return normalizeStaticBearerForCreate(fields)
	case "environment_variable":
		return normalizeEnvironmentVariableForCreate(fields)
	default:
		return credentialAuthState{}, errors.New("auth.type must be mcp_oauth, static_bearer, or environment_variable")
	}
}

func normalizeMCPOAuthForCreate(fields map[string]json.RawMessage) (credentialAuthState, error) {
	serverURL, err := requiredString(fields, "mcp_server_url", "auth.mcp_server_url")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
		return credentialAuthState{}, err
	}
	accessToken, err := requiredString(fields, "access_token", "auth.access_token")
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := map[string]any{
		"type":           "mcp_oauth",
		"mcp_server_url": serverURL,
	}
	secretPayload := map[string]any{
		"type":         "mcp_oauth",
		"access_token": accessToken,
	}
	if expiresAt, ok, err := optionalString(fields, "expires_at", "auth.expires_at"); err != nil {
		return credentialAuthState{}, err
	} else if ok {
		if err := validateRFC3339(expiresAt, "auth.expires_at"); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["expires_at"] = expiresAt
	}
	if rawRefresh, ok := fields["refresh"]; ok && !isJSONNull(rawRefresh) {
		publicRefresh, secretRefresh, err := normalizeMCPOAuthRefreshForCreate(rawRefresh)
		if err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["refresh"] = publicRefresh
		secretPayload["refresh"] = secretRefresh
	}
	return credentialAuthStateFromMaps("mcp_oauth", serverURL, publicAuth, secretPayload)
}

func normalizeMCPOAuthRefreshForCreate(raw json.RawMessage) (map[string]any, map[string]any, error) {
	fields, err := objectFromRaw(raw, "auth.refresh")
	if err != nil {
		return nil, nil, err
	}
	tokenEndpoint, err := requiredString(fields, "token_endpoint", "auth.refresh.token_endpoint")
	if err != nil {
		return nil, nil, err
	}
	if err := validateHTTPURL(tokenEndpoint, "auth.refresh.token_endpoint"); err != nil {
		return nil, nil, err
	}
	clientID, err := requiredString(fields, "client_id", "auth.refresh.client_id")
	if err != nil {
		return nil, nil, err
	}
	refreshToken, err := requiredString(fields, "refresh_token", "auth.refresh.refresh_token")
	if err != nil {
		return nil, nil, err
	}
	publicTokenAuth, secretTokenAuth, err := normalizeTokenEndpointAuthForCreate(fields["token_endpoint_auth"])
	if err != nil {
		return nil, nil, err
	}
	publicRefresh := map[string]any{
		"token_endpoint":      tokenEndpoint,
		"client_id":           clientID,
		"token_endpoint_auth": publicTokenAuth,
	}
	secretRefresh := map[string]any{
		"refresh_token":       refreshToken,
		"token_endpoint_auth": secretTokenAuth,
	}
	if value, ok, err := optionalString(fields, "scope", "auth.refresh.scope"); err != nil {
		return nil, nil, err
	} else if ok {
		publicRefresh["scope"] = value
	}
	if value, ok, err := optionalString(fields, "resource", "auth.refresh.resource"); err != nil {
		return nil, nil, err
	} else if ok {
		publicRefresh["resource"] = value
	}
	return publicRefresh, secretRefresh, nil
}

func normalizeTokenEndpointAuthForCreate(raw json.RawMessage) (map[string]any, map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{"type": "none"}, map[string]any{"type": "none"}, nil
	}
	fields, err := objectFromRaw(raw, "auth.refresh.token_endpoint_auth")
	if err != nil {
		return nil, nil, err
	}
	authType, err := requiredString(fields, "type", "auth.refresh.token_endpoint_auth.type")
	if err != nil {
		return nil, nil, err
	}
	switch authType {
	case "none":
		return map[string]any{"type": "none"}, map[string]any{"type": "none"}, nil
	case "client_secret_basic", "client_secret_post":
		clientSecret, err := requiredString(fields, "client_secret", "auth.refresh.token_endpoint_auth.client_secret")
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"type": authType}, map[string]any{"type": authType, "client_secret": clientSecret}, nil
	default:
		return nil, nil, errors.New("auth.refresh.token_endpoint_auth.type must be none, client_secret_basic, or client_secret_post")
	}
}

func normalizeStaticBearerForCreate(fields map[string]json.RawMessage) (credentialAuthState, error) {
	serverURL, err := requiredString(fields, "mcp_server_url", "auth.mcp_server_url")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
		return credentialAuthState{}, err
	}
	token, err := requiredString(fields, "token", "auth.token")
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := map[string]any{"type": "static_bearer", "mcp_server_url": serverURL}
	secretPayload := map[string]any{"type": "static_bearer", "token": token}
	return credentialAuthStateFromMaps("static_bearer", serverURL, publicAuth, secretPayload)
}

func normalizeEnvironmentVariableForCreate(fields map[string]json.RawMessage) (credentialAuthState, error) {
	secretName, err := requiredString(fields, "secret_name", "auth.secret_name")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateSecretName(secretName); err != nil {
		return credentialAuthState{}, err
	}
	secretValue, err := requiredString(fields, "secret_value", "auth.secret_value")
	if err != nil {
		return credentialAuthState{}, err
	}
	networking, err := normalizeCredentialNetworking(fields["networking"])
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := map[string]any{
		"type":        "environment_variable",
		"secret_name": secretName,
		"networking":  networking,
	}
	secretPayload := map[string]any{"type": "environment_variable", "secret_value": secretValue}
	return credentialAuthStateFromMaps("environment_variable", secretName, publicAuth, secretPayload)
}

func normalizeCredentialAuthForUpdate(current db.VaultCredential, raw json.RawMessage) (credentialAuthState, error) {
	fields, err := objectFromRaw(raw, "auth")
	if err != nil {
		return credentialAuthState{}, err
	}
	authType, err := requiredString(fields, "type", "auth.type")
	if err != nil {
		return credentialAuthState{}, err
	}
	if authType != current.AuthType {
		return credentialAuthState{}, errors.New("auth.type cannot be changed")
	}
	publicAuth := rawObjectMap(current.Auth)
	secretPayload := rawObjectMap(current.SecretPayload)
	if secretPayload == nil {
		secretPayload = map[string]any{"type": current.AuthType}
	}
	publicAuth["type"] = current.AuthType
	secretPayload["type"] = current.AuthType

	switch current.AuthType {
	case "mcp_oauth":
		if _, ok := fields["mcp_server_url"]; ok {
			return credentialAuthState{}, errors.New("auth.mcp_server_url is immutable")
		}
		if rawAccessToken, ok := fields["access_token"]; ok {
			accessToken, err := rawString(rawAccessToken, "auth.access_token")
			if err != nil {
				return credentialAuthState{}, err
			}
			secretPayload["access_token"] = accessToken
		}
		if rawExpiresAt, ok := fields["expires_at"]; ok {
			expiresAt, err := rawString(rawExpiresAt, "auth.expires_at")
			if err != nil {
				return credentialAuthState{}, err
			}
			if err := validateRFC3339(expiresAt, "auth.expires_at"); err != nil {
				return credentialAuthState{}, err
			}
			publicAuth["expires_at"] = expiresAt
		}
		if rawRefresh, ok := fields["refresh"]; ok {
			if isJSONNull(rawRefresh) {
				delete(publicAuth, "refresh")
				delete(secretPayload, "refresh")
			} else if err := patchMCPOAuthRefreshForUpdate(publicAuth, secretPayload, rawRefresh); err != nil {
				return credentialAuthState{}, err
			}
		}
		return credentialAuthStateFromMaps(current.AuthType, current.CredentialKey, publicAuth, secretPayload)
	case "static_bearer":
		if _, ok := fields["mcp_server_url"]; ok {
			return credentialAuthState{}, errors.New("auth.mcp_server_url is immutable")
		}
		if rawToken, ok := fields["token"]; ok {
			token, err := rawString(rawToken, "auth.token")
			if err != nil {
				return credentialAuthState{}, err
			}
			secretPayload["token"] = token
		}
		return credentialAuthStateFromMaps(current.AuthType, current.CredentialKey, publicAuth, secretPayload)
	case "environment_variable":
		if _, ok := fields["secret_name"]; ok {
			return credentialAuthState{}, errors.New("auth.secret_name is immutable")
		}
		if rawSecretValue, ok := fields["secret_value"]; ok {
			secretValue, err := rawString(rawSecretValue, "auth.secret_value")
			if err != nil {
				return credentialAuthState{}, err
			}
			secretPayload["secret_value"] = secretValue
		}
		if rawNetworking, ok := fields["networking"]; ok {
			networking, err := normalizeCredentialNetworking(rawNetworking)
			if err != nil {
				return credentialAuthState{}, err
			}
			publicAuth["networking"] = networking
		}
		return credentialAuthStateFromMaps(current.AuthType, current.CredentialKey, publicAuth, secretPayload)
	default:
		return credentialAuthState{}, errors.New("stored credential auth type is invalid")
	}
}

func patchMCPOAuthRefreshForUpdate(publicAuth, secretPayload map[string]any, raw json.RawMessage) error {
	fields, err := objectFromRaw(raw, "auth.refresh")
	if err != nil {
		return err
	}
	if _, ok := fields["token_endpoint"]; ok {
		return errors.New("auth.refresh.token_endpoint is immutable")
	}
	if _, ok := fields["client_id"]; ok {
		return errors.New("auth.refresh.client_id is immutable")
	}
	if _, ok := fields["resource"]; ok {
		return errors.New("auth.refresh.resource is immutable")
	}
	publicRefresh := nestedMap(publicAuth, "refresh")
	secretRefresh := nestedMap(secretPayload, "refresh")
	if publicRefresh == nil {
		return errors.New("auth.refresh cannot be added after creation")
	}
	if secretRefresh == nil {
		secretRefresh = map[string]any{}
	}
	if rawRefreshToken, ok := fields["refresh_token"]; ok {
		refreshToken, err := rawString(rawRefreshToken, "auth.refresh.refresh_token")
		if err != nil {
			return err
		}
		secretRefresh["refresh_token"] = refreshToken
	}
	if rawScope, ok := fields["scope"]; ok {
		if isJSONNull(rawScope) {
			delete(publicRefresh, "scope")
		} else {
			scope, err := rawString(rawScope, "auth.refresh.scope")
			if err != nil {
				return err
			}
			publicRefresh["scope"] = scope
		}
	}
	if rawTokenAuth, ok := fields["token_endpoint_auth"]; ok {
		publicTokenAuth, secretTokenAuth, err := normalizeTokenEndpointAuthForUpdate(rawTokenAuth)
		if err != nil {
			return err
		}
		publicRefresh["token_endpoint_auth"] = publicTokenAuth
		secretRefresh["token_endpoint_auth"] = secretTokenAuth
	}
	publicAuth["refresh"] = publicRefresh
	secretPayload["refresh"] = secretRefresh
	return nil
}

func normalizeTokenEndpointAuthForUpdate(raw json.RawMessage) (map[string]any, map[string]any, error) {
	if isJSONNull(raw) {
		return map[string]any{"type": "none"}, map[string]any{"type": "none"}, nil
	}
	fields, err := objectFromRaw(raw, "auth.refresh.token_endpoint_auth")
	if err != nil {
		return nil, nil, err
	}
	authType, err := requiredString(fields, "type", "auth.refresh.token_endpoint_auth.type")
	if err != nil {
		return nil, nil, err
	}
	switch authType {
	case "none":
		return map[string]any{"type": "none"}, map[string]any{"type": "none"}, nil
	case "client_secret_basic", "client_secret_post":
		clientSecret, err := requiredString(fields, "client_secret", "auth.refresh.token_endpoint_auth.client_secret")
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"type": authType}, map[string]any{"type": authType, "client_secret": clientSecret}, nil
	default:
		return nil, nil, errors.New("auth.refresh.token_endpoint_auth.type must be none, client_secret_basic, or client_secret_post")
	}
}

func normalizeCredentialNetworking(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{"type": "unrestricted"}, nil
	}
	fields, err := objectFromRaw(raw, "auth.networking")
	if err != nil {
		return nil, err
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = "unrestricted"
	}
	switch networkType {
	case "unrestricted":
		return map[string]any{"type": "unrestricted"}, nil
	case "limited":
		hosts := []string{}
		if rawHosts, ok := fields["allowed_hosts"]; ok && !isJSONNull(rawHosts) {
			values, err := stringArray(rawHosts, "auth.networking.allowed_hosts")
			if err != nil {
				return nil, err
			}
			if len(values) > 16 {
				return nil, errors.New("auth.networking.allowed_hosts must contain at most 16 hosts")
			}
			for _, host := range values {
				if err := validateCredentialHost(host); err != nil {
					return nil, err
				}
			}
			hosts = values
		}
		return map[string]any{"type": "limited", "allowed_hosts": hosts}, nil
	default:
		return nil, errors.New("auth.networking.type must be unrestricted or limited")
	}
}

func credentialAuthStateFromMaps(authType, key string, publicAuth, secretPayload map[string]any) (credentialAuthState, error) {
	publicRaw, err := marshalRaw(publicAuth)
	if err != nil {
		return credentialAuthState{}, err
	}
	secretRaw, err := marshalRaw(secretPayload)
	if err != nil {
		return credentialAuthState{}, err
	}
	return credentialAuthState{
		AuthType:      authType,
		Key:           key,
		PublicAuth:    publicRaw,
		SecretPayload: secretRaw,
	}, nil
}

func objectFromRaw(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s is required", name)
	}
	if isJSONNull(raw) {
		return nil, fmt.Errorf("%s cannot be null", name)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	return fields, nil
}

func requiredString(fields map[string]json.RawMessage, key, name string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return rawString(raw, name)
}

func optionalString(fields map[string]json.RawMessage, key, name string) (string, bool, error) {
	raw, ok := fields[key]
	if !ok || isJSONNull(raw) {
		return "", false, nil
	}
	value, err := rawString(raw, name)
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func rawString(raw json.RawMessage, name string) (string, error) {
	if isJSONNull(raw) {
		return "", fmt.Errorf("%s cannot be null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

func rawStringOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func stringArray(raw json.RawMessage, name string) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s entries must be non-empty strings", name)
		}
		if len(value) > 253 {
			return nil, fmt.Errorf("%s entries must be at most 253 characters", name)
		}
	}
	return values, nil
}

func validateHTTPURL(value, name string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid URL", name)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", name)
	}
	return nil
}

func validateRFC3339(value, name string) error {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be RFC3339", name)
	}
	return nil
}

func validateSecretName(value string) error {
	if len(value) > 255 {
		return errors.New("auth.secret_name must be at most 255 characters")
	}
	return nil
}

func validateCredentialHost(host string) error {
	if strings.Contains(host, "://") || strings.Contains(host, "/") || strings.Contains(host, ":") || strings.Contains(host, "[") || strings.Contains(host, "]") {
		return errors.New("auth.networking.allowed_hosts entries must be hostnames without URL schemes")
	}
	if len(host) > 253 {
		return errors.New("auth.networking.allowed_hosts entries must be at most 253 characters")
	}
	if !credentialHostPattern.MatchString(host) {
		return errors.New("auth.networking.allowed_hosts entries must be valid hostnames")
	}
	return nil
}

func rawObjectMap(raw json.RawMessage) map[string]any {
	var value map[string]any
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{}
	}
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return map[string]any{}
	}
	return value
}

func nestedMap(parent map[string]any, key string) map[string]any {
	value, ok := parent[key]
	if !ok || value == nil {
		return nil
	}
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var mapped map[string]any
	if err := json.Unmarshal(raw, &mapped); err != nil {
		return nil
	}
	return mapped
}

func hasNestedSecret(raw json.RawMessage, parent, child string) bool {
	root := rawObjectMap(raw)
	nested := nestedMap(root, parent)
	if nested == nil {
		return false
	}
	value, ok := nested[child].(string)
	return ok && strings.TrimSpace(value) != ""
}

func fieldOrDefault(fields map[string]json.RawMessage, name, fallback string) json.RawMessage {
	if raw, ok := fields[name]; ok {
		return raw
	}
	return json.RawMessage(fallback)
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
	data, _ := json.Marshal(map[string]any{"created_at": formatTime(vault.CreatedAt), "id": vault.ID})
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
		ID        int64  `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.ID <= 0 || payload.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.VaultPageCursor{CreatedAt: createdAt.UTC(), ID: payload.ID}, nil
}

func encodeCredentialCursor(credential db.VaultCredential) string {
	data, _ := json.Marshal(map[string]any{"created_at": formatTime(credential.CreatedAt), "id": credential.ID})
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
		ID        int64  `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.ID <= 0 || payload.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.VaultCredentialPageCursor{CreatedAt: createdAt.UTC(), ID: payload.ID}, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
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
