import { consoleApi } from '../../../shared/api/client';

export type BusinessIdentity = {
  id: string;
  namespace: string;
  external_subject_id: string;
  display_name: string;
  enabled: boolean;
  created_at: string;
};

export type IdentityDirectoryRow = BusinessIdentity & {
  active: boolean;
  agents: number;
  sessions: number;
  last_used_at: string | null;
};

export type IdentityPage = {
  data: IdentityDirectoryRow[];
  total_count: number;
  overview: { identities: number; active: number; agents: number; sessions: number };
  agent_options: { id: string; name: string }[];
};

export type AgentIdentityBinding = {
  identity_id: string;
  display_name: string;
  channel_id: string;
  scope_kind: string;
  remote_id: string;
  pairing_id: string;
  status: string;
};

export function identityBase(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}`;
}

export function listIdentities(orgUuid: string, workspaceId: string, params: URLSearchParams, signal: AbortSignal) {
  return consoleApi<IdentityPage>(`${identityBase(orgUuid, workspaceId)}/identities/directory?${params}`, { signal });
}

export function listAgentIdentityBindings(orgUuid: string, workspaceId: string, agentId: string) {
  return consoleApi<{ data: AgentIdentityBinding[] }>(
    `${identityBase(orgUuid, workspaceId)}/agents/${encodeURIComponent(agentId)}/identity_bindings`,
  );
}

export type IdentityAgentChannel = {
  id: string;
  channel_type: string;
  name: string;
  binding_status: 'bound' | 'unbound' | 'expired';
};

export function listIdentityAgents(orgUuid: string, workspaceId: string, identityId: string, signal: AbortSignal) {
  return consoleApi<{ data: { agent_id: string; sessions: number; channels: IdentityAgentChannel[] }[] }>(
    `${identityBase(orgUuid, workspaceId)}/identities/${encodeURIComponent(identityId)}/agents?window=all&include_channels=true`,
    { signal },
  );
}
