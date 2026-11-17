import { consoleApi } from '../../../shared/api/client';

export type BusinessIdentity = {
  id: string;
  namespace: string;
  external_subject_id: string;
  display_name: string;
  enabled: boolean;
  auto_memory_store: boolean;
};

export type IdentityPage = {
  data: BusinessIdentity[];
  next_page?: string | null;
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

export function listIdentities(orgUuid: string, workspaceId: string) {
  return consoleApi<IdentityPage>(`${identityBase(orgUuid, workspaceId)}/identities`);
}

export function createIdentity(
  orgUuid: string,
  workspaceId: string,
  input: { namespace: string; external_subject_id: string; display_name: string },
) {
  return consoleApi<BusinessIdentity>(`${identityBase(orgUuid, workspaceId)}/identities`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function setIdentityEnabled(orgUuid: string, workspaceId: string, identityId: string, enabled: boolean) {
  const action = enabled ? 'enable' : 'disable';
  return consoleApi<BusinessIdentity>(
    `${identityBase(orgUuid, workspaceId)}/identities/${encodeURIComponent(identityId)}/${action}`,
    {
      method: 'POST',
    },
  );
}

export function listAgentIdentityBindings(orgUuid: string, workspaceId: string, agentId: string) {
  return consoleApi<{ data: AgentIdentityBinding[] }>(
    `${identityBase(orgUuid, workspaceId)}/agents/${encodeURIComponent(agentId)}/identity_bindings`,
  );
}
