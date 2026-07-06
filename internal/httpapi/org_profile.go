package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type OrganizationProfileStore interface {
	GetOrganizationProfile(ctx context.Context, orgUUID string) (OrganizationProfile, error)
	UpdateOrganizationProfile(ctx context.Context, orgUUID string, profile OrganizationProfile) (OrganizationProfile, error)
}

func RegisterOrganizationProfileRoutes(r chi.Router, store OrganizationProfileStore) {
	r.Get("/profile", handleGetOrganizationProfile(store))
	r.Put("/profile", handleUpdateOrganizationProfile(store))
}

func handleGetOrganizationProfile(store OrganizationProfileStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if store == nil {
			internalError(w, "failed to load organization profile")
			return
		}
		profile, err := store.GetOrganizationProfile(r.Context(), orgUUID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to load organization profile")
			return
		}
		writeJSON(w, http.StatusOK, profile)
	}
}

func handleUpdateOrganizationProfile(store OrganizationProfileStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if store == nil {
			internalError(w, "failed to save organization profile")
			return
		}
		body, err := readJSONObject(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body must be an object"})
			return
		}
		existing, err := store.GetOrganizationProfile(r.Context(), orgUUID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to load organization profile")
			return
		}
		saved, err := store.UpdateOrganizationProfile(r.Context(), orgUUID, applyOrganizationProfilePatch(existing, body))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to save organization profile")
			return
		}
		writeJSON(w, http.StatusOK, saved)
	}
}

func applyOrganizationProfilePatch(existing OrganizationProfile, body map[string]any) OrganizationProfile {
	profile := cloneOrganizationProfile(existing)
	if value, ok := body["physical_address"]; ok {
		profile.PhysicalAddress = normalizeOrganizationProfileAddress(value)
	}
	if value, ok := body["tax_id"]; ok {
		profile.TaxID = normalizeOrganizationProfileTaxID(value)
	}
	if removeTaxID, ok := body["remove_tax_id"].(bool); ok && removeTaxID {
		profile.TaxID = nil
	}
	applyOrganizationProfileStringField(&profile, body, "website")
	applyOrganizationProfileStringField(&profile, body, "industry")
	applyOrganizationProfileStringField(&profile, body, "bill_to")
	return profile
}

func cloneOrganizationProfile(profile OrganizationProfile) OrganizationProfile {
	out := profile
	if profile.PhysicalAddress != nil {
		address := *profile.PhysicalAddress
		address.Line2 = cloneOrgStringPtr(profile.PhysicalAddress.Line2)
		out.PhysicalAddress = &address
	}
	if profile.TaxID != nil {
		taxID := *profile.TaxID
		taxID.Country = cloneOrgStringPtr(profile.TaxID.Country)
		out.TaxID = &taxID
	}
	out.Website = cloneOrgStringPtr(profile.Website)
	out.Industry = cloneOrgStringPtr(profile.Industry)
	out.BillTo = cloneOrgStringPtr(profile.BillTo)
	return out
}

func normalizeOrganizationProfileAddress(value any) *OrganizationPhysicalAddress {
	body, ok := value.(map[string]any)
	if !ok || body == nil {
		return nil
	}
	return &OrganizationPhysicalAddress{
		Line1:      organizationProfileString(body["line1"]),
		Line2:      organizationProfileStringPtr(body["line2"]),
		City:       organizationProfileString(body["city"]),
		State:      organizationProfileString(body["state"]),
		Country:    organizationProfileString(body["country"]),
		PostalCode: organizationProfileString(body["postal_code"]),
	}
}

func normalizeOrganizationProfileTaxID(value any) *OrganizationTaxID {
	body, ok := value.(map[string]any)
	if !ok || body == nil {
		return nil
	}
	taxType := organizationProfileString(body["type"])
	taxValue := organizationProfileString(body["value"])
	if taxType == "" || taxValue == "" {
		return nil
	}
	return &OrganizationTaxID{
		Type:    taxType,
		Value:   taxValue,
		Country: organizationProfileStringPtr(body["country"]),
	}
}

func applyOrganizationProfileStringField(profile *OrganizationProfile, body map[string]any, key string) {
	if _, ok := body[key]; !ok {
		return
	}
	value := organizationProfileStringPtr(body[key])
	switch key {
	case "website":
		profile.Website = value
	case "industry":
		profile.Industry = value
	case "bill_to":
		profile.BillTo = value
	}
}

func organizationProfileString(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func organizationProfileStringPtr(value any) *string {
	normalized := organizationProfileString(value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func cloneOrgStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
