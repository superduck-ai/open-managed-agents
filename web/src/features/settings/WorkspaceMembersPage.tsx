import type { UseMutationResult, UseQueryResult } from '@tanstack/react-query';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Info, Plus, UsersRound } from 'lucide-react';
import { useState } from 'react';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Button, ButtonLink } from '../../shared/ui/button';
import { useWorkspace } from '../../shared/workspaces/context';
import { ConsolePageFrame } from '../dashboard/frame';
import { MemberDirectoryFilters } from './MemberDirectoryFilters';
import { MemberRemovalDialog } from './MemberRemovalDialog';
import { AddWorkspaceMemberDialog } from './workspace-members/AddWorkspaceMemberDialog';
import { WorkspaceMemberTable, type MemberSort } from './workspace-members/WorkspaceMemberTable';
import {
  type WorkspaceMemberDirectory,
  changeWorkspaceMember,
  listWorkspaceMembers,
  workspaceMemberRoles,
  type WorkspaceMember,
  type WorkspaceMemberRole,
} from './workspace-members/api';

function resolveOrganizationName(
  account:
    | {
        memberships?: Array<{ organization?: { uuid?: string; name?: string } }>;
      }
    | null
    | undefined,
  orgUuid: string | null | undefined,
): string {
  return (
    account?.memberships?.find((membership) => membership.organization?.uuid === orgUuid)?.organization?.name ??
    'this organization'
  );
}

export function WorkspaceMembersPage() {
  const { msg } = useI18n();
  const { account, csrfToken } = useAuth();
  const queryClient = useQueryClient();
  const { activeWorkspace, orgUuid, isLoading } = useWorkspace();
  const isDefault = activeWorkspace.is_default === true;
  const workspaceId = activeWorkspace.external_id || activeWorkspace.id;
  const [search, setSearch] = useState('');
  const [roleFilter, setRoleFilter] = useState('all');
  const [sort, setSort] = useState<MemberSort>({ field: 'name', descending: false });
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<WorkspaceMember | null>(null);
  const organizationName = resolveOrganizationName(account, orgUuid);

  const directoryKey = ['console', 'workspace-members', orgUuid, workspaceId] as const;
  const directory = useQuery({
    queryKey: directoryKey,
    queryFn: () => listWorkspaceMembers(orgUuid ?? '', workspaceId),
    enabled: !isLoading && Boolean(orgUuid && workspaceId) && !isDefault,
    retry: false,
  });
  const mutation = useMutation({
    mutationFn: (input: { operation: 'create' | 'update' | 'delete'; userId: string; role: WorkspaceMemberRole }) =>
      changeWorkspaceMember(orgUuid ?? '', workspaceId, input.operation, input.userId, input.role, csrfToken),
    onSuccess: async () => {
      setAdding(false);
      setRemoving(null);
      await queryClient.invalidateQueries({ queryKey: directoryKey });
    },
  });
  const members = (directory.data?.members ?? [])
    .filter(
      (member) =>
        `${member.name} ${member.email}`.toLowerCase().includes(search.toLowerCase()) &&
        (roleFilter === 'all' || member.workspace_role === roleFilter),
    )
    .sort((left, right) => left[sort.field].localeCompare(right[sort.field]) * (sort.descending ? -1 : 1));

  return (
    <ConsolePageFrame
      title={msg('members.title', 'Members')}
      icon={UsersRound}
      description={
        directory.data && !directory.data.is_default
          ? msg(
              'members.workspaceNotice',
              'Organization admins inherit the workspace admin role. Billing members can be promoted to workspace admin.',
            )
          : undefined
      }
      actions={
        directory.data?.can_manage_members && !isDefault ? (
          <Button
            onClick={() => {
              mutation.reset();
              setAdding(true);
            }}
          >
            <Plus className="size-4" aria-hidden />
            {msg('members.addToWorkspace', 'Add to Workspace')}
          </Button>
        ) : null
      }
    >
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
      ) : (
        <WorkspaceMemberDirectoryPanel
          directory={directory}
          mutation={mutation}
          members={members}
          search={search}
          onSearch={setSearch}
          roleFilter={roleFilter}
          onRoleFilter={setRoleFilter}
          sort={sort}
          onSort={setSort}
          dialogOpen={adding || Boolean(removing)}
          onRole={(member, role) => mutation.mutate({ operation: 'update', userId: member.user_id, role })}
          onRemove={(member) => {
            mutation.reset();
            setRemoving(member);
          }}
        />
      )}
      {adding ? (
        <AddWorkspaceMemberDialog
          orgUuid={orgUuid ?? ''}
          workspaceId={workspaceId}
          open
          onClose={() => setAdding(false)}
          pending={mutation.isPending}
          error={mutation.error?.message}
          onAdd={(userId, role) => mutation.mutate({ operation: 'create', userId, role })}
        />
      ) : null}
      <MemberRemovalDialog
        open={Boolean(removing)}
        title={msg('members.removeDialogTitle', 'Remove member?')}
        pending={mutation.isPending}
        error={mutation.error?.message}
        onClose={() => setRemoving(null)}
        onConfirm={() => {
          if (removing)
            mutation.mutate({ operation: 'delete', userId: removing.user_id, role: removing.workspace_role });
        }}
      >
        {msg('members.removeDialogBody', 'Are you sure you want to remove {email} from {organization}?', {
          email: removing?.email ?? '',
          organization: organizationName,
        })}
      </MemberRemovalDialog>
    </ConsolePageFrame>
  );
}

function WorkspaceMemberDirectoryPanel({
  directory,
  mutation,
  members,
  search,
  onSearch,
  roleFilter,
  onRoleFilter,
  sort,
  onSort,
  dialogOpen,
  onRole,
  onRemove,
}: {
  directory: UseQueryResult<WorkspaceMemberDirectory, Error>;
  mutation: UseMutationResult<
    unknown,
    Error,
    { operation: 'create' | 'update' | 'delete'; userId: string; role: WorkspaceMemberRole }
  >;
  members: WorkspaceMember[];
  search: string;
  onSearch: (value: string) => void;
  roleFilter: string;
  onRoleFilter: (value: string) => void;
  sort: MemberSort;
  onSort: (sort: MemberSort) => void;
  dialogOpen: boolean;
  onRole: (member: WorkspaceMember, role: WorkspaceMemberRole) => void;
  onRemove: (member: WorkspaceMember) => void;
}) {
  return (
    <>
      <MemberDirectoryFilters
        search={search}
        onSearch={onSearch}
        role={roleFilter}
        onRole={onRoleFilter}
        roles={workspaceMemberRoles}
      />
      {directory.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{directory.error.message}</AlertDescription>
        </Alert>
      ) : null}
      {mutation.isError && !dialogOpen ? (
        <Alert variant="destructive">
          <AlertDescription>{mutation.error.message}</AlertDescription>
        </Alert>
      ) : null}
      {!directory.isError ? (
        <WorkspaceMemberTable
          members={members}
          loading={directory.isPending}
          failed={false}
          pending={mutation.isPending}
          sort={sort}
          setSort={onSort}
          onRole={onRole}
          onRemove={onRemove}
        />
      ) : null}
    </>
  );
}
