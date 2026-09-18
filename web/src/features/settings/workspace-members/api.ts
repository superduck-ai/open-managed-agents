import { consoleApi } from '../../../shared/api/client';

export type WorkspaceMemberRole =
  'workspace_user' | 'workspace_developer' | 'workspace_restricted_developer' | 'workspace_admin' | 'workspace_billing';
export type WorkspaceMember = {
  user_id: string;
  name: string;
  email: string;
  organization_role: string;
  workspace_role: WorkspaceMemberRole;
  role_source: string;
  can_edit: boolean;
  can_remove: boolean;
};
export type WorkspaceMemberDirectory = {
  workspace_id: string;
  is_default: boolean;
  can_manage_members: boolean;
  members: WorkspaceMember[];
};
export type WorkspaceMemberCandidate = { user_id: string; name: string; email: string };

function workspaceMembersPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}`;
}
export function listWorkspaceMembers(orgUuid: string, workspaceId: string) {
  return consoleApi<WorkspaceMemberDirectory>(`${workspaceMembersPath(orgUuid, workspaceId)}/members`);
}
export function listWorkspaceMemberCandidates(orgUuid: string, workspaceId: string) {
  return consoleApi<WorkspaceMemberCandidate[]>(`${workspaceMembersPath(orgUuid, workspaceId)}/member-candidates`);
}
export function changeWorkspaceMember(
  orgUuid: string,
  workspaceId: string,
  operation: 'create' | 'update' | 'delete',
  userId: string,
  role: WorkspaceMemberRole,
  csrfToken?: string,
) {
  const path = `${workspaceMembersPath(orgUuid, workspaceId)}/members${operation === 'create' ? '' : `/${encodeURIComponent(userId)}`}`;
  return consoleApi<unknown>(path, {
    method: operation === 'delete' ? 'DELETE' : 'POST',
    csrfToken,
    body:
      operation === 'delete'
        ? undefined
        : JSON.stringify({ ...(operation === 'create' ? { user_id: userId } : {}), workspace_role: role }),
  });
}

export const workspaceMemberRoles: { value: WorkspaceMemberRole; label: string }[] = [
  { value: 'workspace_user', label: 'Workspace User' },
  { value: 'workspace_restricted_developer', label: 'Workspace Limited Developer' },
  { value: 'workspace_developer', label: 'Workspace Developer' },
  { value: 'workspace_admin', label: 'Workspace Admin' },
  { value: 'workspace_billing', label: 'Workspace Billing' },
];
