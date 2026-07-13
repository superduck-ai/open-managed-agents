import { consoleApi } from '../../shared/api/client';
import type { PlatformRole } from '../../shared/permissions/roles';

export type OrganizationMember = {
  id: string;
  type?: 'user';
  email: string;
  name: string;
  role: PlatformRole | string;
  added_at?: string;
};

export type OrganizationInvite = {
  id: string;
  type?: 'invite';
  email: string;
  role: PlatformRole | string;
  invited_at: string;
  expires_at: string;
  status: 'pending' | 'accepted' | 'expired' | 'deleted' | string;
};

export type DeletedOrganizationInvite = {
  id: string;
  type: 'invite_deleted';
};

export function listOrganizationMembers(orgUuid: string) {
  return consoleApi<OrganizationMember[]>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/members`);
}

export function listOrganizationInvites(orgUuid: string, status = 'pending') {
  const query = status ? `?status=${encodeURIComponent(status)}` : '';
  return consoleApi<OrganizationInvite[]>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/invites${query}`);
}

export function createOrganizationInvite(orgUuid: string, email: string, role: PlatformRole, csrfToken?: string) {
  return consoleApi<OrganizationInvite>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/invites`, {
    method: 'POST',
    csrfToken,
    body: JSON.stringify({ email, role }),
  });
}

export function resendOrganizationInvite(orgUuid: string, inviteId: string, csrfToken?: string) {
  return consoleApi<OrganizationInvite>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/invites/${encodeURIComponent(inviteId)}`,
    {
      method: 'PUT',
      csrfToken,
    },
  );
}

export function deleteOrganizationInvite(orgUuid: string, inviteId: string, csrfToken?: string) {
  return consoleApi<DeletedOrganizationInvite>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/invites/${encodeURIComponent(inviteId)}`,
    {
      method: 'DELETE',
      csrfToken,
    },
  );
}

export function updateOrganizationMemberRole(
  orgUuid: string,
  memberId: string,
  role: PlatformRole,
  csrfToken?: string,
) {
  return consoleApi<OrganizationMember>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/members/${encodeURIComponent(memberId)}`,
    {
      method: 'POST',
      csrfToken,
      body: JSON.stringify({ role }),
    },
  );
}
