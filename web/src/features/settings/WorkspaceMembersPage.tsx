import { useQuery } from '@tanstack/react-query';
import { Info, UsersRound } from 'lucide-react';
import { consoleApi } from '../../shared/api/client';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { ButtonLink } from '../../shared/ui/button';
import { useWorkspace } from '../../shared/workspaces/context';
import { ConsolePageFrame, DataTable } from '../dashboard/frame';

type WorkspaceMember = { user_id: string; workspace_role: string };
type MemberPage = { data: WorkspaceMember[]; has_more: boolean; last_id: string };

async function listMembers(workspaceId: string) {
  const members: WorkspaceMember[] = [];
  let afterId = '';
  do {
    const query = new URLSearchParams({ limit: '100' });
    if (afterId) query.set('after_id', afterId);
    const page = await consoleApi<MemberPage>(
      `/v1/organizations/workspaces/${encodeURIComponent(workspaceId)}/members?${query}`,
      { headers: { 'X-Workspace-ID': workspaceId } },
    );
    members.push(...page.data);
    afterId = page.has_more ? page.last_id : '';
  } while (afterId);
  return members;
}

export function WorkspaceMembersPage() {
  const { msg } = useI18n();
  const { activeWorkspace, orgUuid, isLoading } = useWorkspace();
  const isDefault = activeWorkspace.is_default === true;
  const workspaceId = activeWorkspace.external_id || activeWorkspace.id;
  const members = useQuery({
    queryKey: ['workspace-members', orgUuid, workspaceId],
    queryFn: () => listMembers(workspaceId),
    enabled: !isLoading && Boolean(workspaceId) && !isDefault,
    retry: false,
  });

  return (
    <ConsolePageFrame title={msg('members.title', 'Members')} icon={UsersRound}>
      {isDefault ? (
        <Alert>
          <Info aria-hidden />
          <AlertDescription className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span>
              {msg('members.defaultNotice', 'Members for the default workspace are managed at the organization level.')}
            </span>
            <ButtonLink href="/settings/members" variant="secondary" className="shrink-0">
              {msg('members.organizationSettings', 'Go to organization settings')}
            </ButtonLink>
          </AlertDescription>
        </Alert>
      ) : members.error ? (
        <Alert variant="destructive">
          <AlertDescription>{members.error.message}</AlertDescription>
        </Alert>
      ) : isLoading || members.isPending ? (
        <p>{msg('common.loading', 'Loading...')}</p>
      ) : (
        <DataTable
          columns={[msg('members.userId', 'User ID'), msg('members.role', 'Role')]}
          rows={(members.data ?? []).map((member) => [member.user_id, member.workspace_role])}
        />
      )}
    </ConsolePageFrame>
  );
}
