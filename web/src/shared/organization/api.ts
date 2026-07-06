import { consoleApi } from '../api/client';

export type OrganizationSettings = {
  default_workspace_settings?: {
    enable_api_keys?: boolean;
  };
  [key: string]: unknown;
};

export type Organization = {
  uuid: string;
  name: string;
  settings?: OrganizationSettings;
  created_at?: string;
  updated_at?: string;
};

export type OrganizationPhysicalAddress = {
  line1: string;
  line2: string | null;
  city: string;
  state: string;
  country: string;
  postal_code: string;
};

export type OrganizationTaxId = {
  type: string;
  value: string;
  country: string | null;
};

export type OrganizationProfile = {
  physical_address: OrganizationPhysicalAddress | null;
  website: string | null;
  industry: string | null;
  tax_id: OrganizationTaxId | null;
  bill_to: string | null;
};

export type UpdateOrganizationInput = {
  name?: string;
  default_workspace_settings?: {
    enable_api_keys?: boolean;
  };
};

export type UpdateOrganizationProfileInput = {
  physical_address?: OrganizationPhysicalAddress | null;
  website?: string | null;
  industry?: string | null;
  tax_id?: OrganizationTaxId | null;
  bill_to?: string | null;
  remove_tax_id?: boolean;
};

export function getOrganization(orgUuid: string) {
  return consoleApi<Organization>(`/api/organizations/${encodeURIComponent(orgUuid)}`);
}

export function updateOrganization(orgUuid: string, input: UpdateOrganizationInput, csrfToken?: string) {
  return consoleApi<Organization>(`/api/organizations/${encodeURIComponent(orgUuid)}`, {
    method: 'PUT',
    csrfToken,
    body: JSON.stringify(input)
  });
}

export function getOrganizationProfile(orgUuid: string) {
  return consoleApi<OrganizationProfile>(`/api/organizations/${encodeURIComponent(orgUuid)}/profile`);
}

export function updateOrganizationProfile(
  orgUuid: string,
  input: UpdateOrganizationProfileInput,
  csrfToken?: string
) {
  return consoleApi<OrganizationProfile>(`/api/organizations/${encodeURIComponent(orgUuid)}/profile`, {
    method: 'PUT',
    csrfToken,
    body: JSON.stringify(input)
  });
}
