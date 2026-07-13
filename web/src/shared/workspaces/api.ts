import { consoleApi } from "../api/client";

export type Workspace = {
  id: string;
  type: "workspace";
  name: string;
  display_color?: string;
  color?: string;
  data_residency?: {
    workspace_geo?: string;
    allowed_inference_geos?: string;
    default_inference_geo?: string;
  } | null;
};

export type CreateWorkspaceInput = {
  name: string;
  display_color: string;
  data_residency: {
    workspace_geo: "us";
  };
};

export type WorkspaceApiKey = {
  type?: "api_key";
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
  status: "active" | "inactive" | "archived";
};

export const defaultWorkspace: Workspace = {
  id: "default",
  type: "workspace",
  name: "Default",
  display_color: "#9B87F5",
  color: "#9B87F5",
  data_residency: {
    workspace_geo: "us",
    allowed_inference_geos: "unrestricted",
    default_inference_geo: "global",
  },
};

export function listConsoleWorkspaces(orgUuid: string) {
  return consoleApi<Workspace[]>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces`);
}

export function createConsoleWorkspace(orgUuid: string, input: CreateWorkspaceInput) {
  return consoleApi<Workspace>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces`, {
    method: "POST",
    body: JSON.stringify(input),
  });
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
      method: "POST",
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
      method: "POST",
      body: JSON.stringify(input),
    },
  );
}
