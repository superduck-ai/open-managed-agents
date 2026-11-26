import { consoleApi } from '../api/client';
import type { OrganizationRuntimeProfile, OrganizationRuntimeProfilesResult, WorkspaceRuntimeSummary } from './runtime';

export type {
  BooleanRuntimeCapabilityKey,
  OrganizationRuntimeProfile,
  OrganizationRuntimeProfilesResult,
  RuntimeArtifactDiscoveryMode,
  RuntimeBudgetMode,
  RuntimeCapabilities,
  RuntimeFileUploadConstraints,
  RuntimeProviderKind,
  RuntimeUsageMeteringMode,
  WorkspaceRuntimeSummary,
} from './runtime';

export type Workspace = {
  is_default?: boolean;
  effective_role?: string;
  role_source?: string;
  external_id?: string;
  archived_at?: string | null;
  id: string;
  type: 'workspace';
  name: string;
  created_at?: string;
  api_keys_count?: number;
  display_color?: string;
  color?: string;
  data_residency?: {
    workspace_geo?: string;
    allowed_inference_geos?: string;
    default_inference_geo?: string;
  } | null;
  runtime?: WorkspaceRuntimeSummary;
};

export type CreateWorkspaceInput = {
  name: string;
  display_color: string;
  data_residency: {
    workspace_geo: 'us';
  };
  runtime_profile_id?: string;
};

export type WorkspaceApiKey = {
  type?: 'api_key';
  id: string;
  workspace_id?: string | null;
  name: string;
  raw_key?: string;
  partial_key_hint?: string;
  key_prefix?: string;
  key_suffix?: string;
  created_by?: { id?: string; type?: string; name?: string; email?: string } | null;
  created_by_user_id?: string | null;
  created_at?: string;
  last_used_at?: string | null;
  expires_at?: string | null;
  archived_at?: string | null;
  status?: string;
  updated_at?: string;
};

export type CreateWorkspaceApiKeyInput = {
  name: string;
};

export type UpdateWorkspaceApiKeyStatusInput = {
  status: 'active' | 'inactive' | 'archived';
};

export const defaultWorkspace: Workspace = {
  id: 'default',
  is_default: true,
  type: 'workspace',
  name: 'Default',
  display_color: '#9B87F5',
  color: '#9B87F5',
  data_residency: {
    workspace_geo: 'us',
    allowed_inference_geos: 'unrestricted',
    default_inference_geo: 'global',
  },
};

export function listConsoleWorkspaces(orgUuid: string, includeArchived = false) {
  return consoleApi<Workspace[]>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces${includeArchived ? '?include_archived=true' : ''}`,
    {
      context: { organizationUuid: orgUuid },
    },
  ).then((workspaces) =>
    workspaces.map((workspace) => (workspace.is_default ? { ...workspace, name: defaultWorkspace.name } : workspace)),
  );
}

export function createConsoleWorkspace(orgUuid: string, input: CreateWorkspaceInput) {
  return consoleApi<Workspace>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

type OrganizationRuntimeProfilesEnvelope =
  | OrganizationRuntimeProfile[]
  | {
      data?: OrganizationRuntimeProfile[];
      profiles?: OrganizationRuntimeProfile[];
      default_runtime_profile_id?: string;
      default_runtime_profile_uuid?: string;
    };

export function listOrganizationRuntimeProfiles(orgUuid: string): Promise<OrganizationRuntimeProfilesResult> {
  return consoleApi<OrganizationRuntimeProfilesEnvelope>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/runtime-profiles`,
    {
      context: { organizationUuid: orgUuid },
    },
  ).then((payload) => {
    const profiles = Array.isArray(payload) ? payload : (payload.profiles ?? payload.data ?? []);
    const defaultProfileUuid = Array.isArray(payload) ? '' : (payload.default_runtime_profile_uuid ?? '');
    const explicitDefaultId = Array.isArray(payload) ? '' : (payload.default_runtime_profile_id ?? '');
    const defaultProfile =
      profiles.find((profile) => profile.is_default) ??
      profiles.find((profile) => (explicitDefaultId ? profile.id === explicitDefaultId : false)) ??
      profiles.find((profile) => (defaultProfileUuid ? profile.uuid === defaultProfileUuid : false)) ??
      profiles[0];
    return {
      profiles,
      defaultProfileId: explicitDefaultId || defaultProfile?.id || defaultProfile?.uuid || '',
      defaultProfileUuid: defaultProfileUuid || defaultProfile?.uuid || '',
    };
  });
}

export async function updateConsoleWorkspace(
  orgUuid: string,
  workspaceId: string,
  input: { name: string; display_color: string },
) {
  return consoleApi<Workspace>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}`,
    { method: 'PATCH', body: JSON.stringify(input) },
  );
}

export async function archiveConsoleWorkspace(orgUuid: string, workspaceId: string) {
  return consoleApi<Workspace>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/archive`,
    { method: 'POST' },
  );
}

export function listWorkspaceApiKeys(orgUuid: string, workspaceId: string) {
  return consoleApi<WorkspaceApiKey[]>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/api_keys`,
  );
}

export function createWorkspaceApiKey(orgUuid: string, workspaceId: string, input: CreateWorkspaceApiKeyInput) {
  return consoleApi<WorkspaceApiKey>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/api_keys`,
    {
      method: 'POST',
      body: JSON.stringify(input),
    },
  );
}

export function updateWorkspaceApiKeyStatus(
  orgUuid: string,
  workspaceId: string,
  apiKeyId: string,
  input: UpdateWorkspaceApiKeyStatusInput,
) {
  return consoleApi<WorkspaceApiKey>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/api_keys/${encodeURIComponent(apiKeyId)}`,
    {
      method: 'POST',
      body: JSON.stringify(input),
    },
  );
}
