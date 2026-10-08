package tunnels

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) createCertificate(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	request, err := httpapi.DecodeObjectBodyAs[createTunnelCertificateRequest](w, r, maxManagementBody)
	if err != nil {
		return invalidRequest(err)
	}
	certificate, err := h.service.CreateCertificate(
		r.Context(),
		scope,
		chi.URLParam(r, "tunnel_id"),
		request.CACertificatePEM,
	)
	if err != nil {
		return err
	}
	h.logManagementSuccess(r, "mcp tunnel certificate created", certificate.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromTunnelCertificate(certificate))
	return nil
}

func (h *Handler) retrieveCertificate(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	certificate, err := h.service.GetCertificate(
		r.Context(),
		scope,
		chi.URLParam(r, "tunnel_id"),
		chi.URLParam(r, "certificate_id"),
	)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromTunnelCertificate(certificate))
	return nil
}

func (h *Handler) listCertificates(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	limit, err := parseListLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseIncludeArchived(r)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	certificates, hasMore, err := h.service.ListCertificates(
		r.Context(),
		scope,
		chi.URLParam(r, "tunnel_id"),
		includeArchived,
		limit,
		offset,
	)
	if err != nil {
		return err
	}
	var nextPage *string
	if hasMore {
		cursor, cursorErr := marshalCursor(offset + limit)
		if cursorErr != nil {
			return internalError(
				"Could not encode tunnel certificates page",
				fmt.Errorf("encode tunnel certificates page: %w", cursorErr),
			)
		}
		nextPage = &cursor
	}
	httpapi.WriteJSON(w, http.StatusOK, tunnelCertificatePageResponse{
		Data:     responsesFromTunnelCertificates(certificates),
		NextPage: nextPage,
	})
	return nil
}

func (h *Handler) archiveCertificate(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	certificate, err := h.service.ArchiveCertificate(
		r.Context(),
		scope,
		chi.URLParam(r, "tunnel_id"),
		chi.URLParam(r, "certificate_id"),
	)
	if err != nil {
		return err
	}
	h.logManagementSuccess(r, "mcp tunnel certificate archived", certificate.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromTunnelCertificate(certificate))
	return nil
}
