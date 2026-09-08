import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useAuth } from '../../../shared/auth/context';
import { useWorkspace } from '../../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../../shared/workspaces/presentation';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { MemberRemovalDialog } from '../MemberRemovalDialog';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { MemberDirectoryFilters } from '../MemberDirectoryFilters';
import { AddWorkspaceMemberDialog } from './AddWorkspaceMemberDialog';
import { WorkspaceMemberTable } from './WorkspaceMemberTable';
import {
  changeWorkspaceMember,
  listWorkspaceMembers,
  workspaceMemberRoles,
  type WorkspaceMemberDirectory,
  type WorkspaceMember,
  type WorkspaceMemberRole,
} from './api';

export function WorkspaceMembersPage() {
  const pathname = useLocation({ select: (location) => location.pathname });
  const workspace = useWorkspace();
  return (
    <WorkspaceMembersContent
      key={`${workspace.orgUuid}:${workspaceIdFromPath(pathname) ?? workspace.activeWorkspaceId}`}
      orgUuid={workspace.orgUuid ?? ''}
      workspaceId={workspaceIdFromPath(pathname) ?? workspace.activeWorkspaceId}
    />
  );
}

export function WorkspaceMembersContent({ orgUuid, workspaceId }: { orgUuid: string; workspaceId: string }) {
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [roleFilter, setRoleFilter] = useState('all');
  const [sort, setSort] = useState<{ field: 'name' | 'email' | 'workspace_role'; descending: boolean }>({
    field: 'name',
    descending: false,
  });
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<WorkspaceMember | null>(null);
  const directoryKey = ['console', 'workspace-members', orgUuid, workspaceId] as const;
  const directory = useQuery({
    queryKey: directoryKey,
    queryFn: () => listWorkspaceMembers(orgUuid, workspaceId),
    enabled: Boolean(orgUuid && workspaceId),
    retry: false,
  });
  const mutation = useMutation({
    mutationFn: (input: { operation: 'create' | 'update' | 'delete'; userId: string; role: WorkspaceMemberRole }) =>
      changeWorkspaceMember(orgUuid, workspaceId, input.operation, input.userId, input.role, csrfToken),
    onSuccess: async () => {
      setAdding(false);
      setRemoving(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: directoryKey }),
        queryClient.invalidateQueries({ queryKey: ['console', 'workspace-member-candidates', orgUuid, workspaceId] }),
      ]);
    },
  });
  const members = (directory.data?.members ?? [])
    .filter(
      (member) =>
        `${member.name} ${member.email}`.toLowerCase().includes(search.toLowerCase()) &&
        (roleFilter === 'all' || member.workspace_role === roleFilter),
    )
    .sort((left, right) => left[sort.field].localeCompare(right[sort.field]) * (sort.descending ? -1 : 1));

  if (!orgUuid) return <p>No organization is available for this session.</p>;
  return (
    <section className="mx-auto w-full max-w-[1180px]" data-testid="workspace-members-page">
      <WorkspaceMemberHeading
        directory={directory.data}
        onAdd={() => {
          mutation.reset();
          setAdding(true);
        }}
      />
      {directory.data?.is_default ? (
        <a className="text-sm underline" href="/settings/members">
          View organization members
        </a>
      ) : (
        <>
          <MemberDirectoryFilters
            search={search}
            onSearch={setSearch}
            role={roleFilter}
            onRole={setRoleFilter}
            roles={workspaceMemberRoles}
          />
          {directory.isError ? (
            <Alert variant="destructive">
              <AlertDescription>
                {directory.error.message}
                <Button variant="outline" onClick={() => void directory.refetch()}>
                  Retry
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          {mutation.isError && !adding && !removing ? (
            <Alert variant="destructive">
              <AlertDescription>{mutation.error.message}</AlertDescription>
            </Alert>
          ) : null}
          <WorkspaceMemberTable
            members={members}
            loading={directory.isLoading}
            failed={directory.isError}
            pending={mutation.isPending}
            sort={sort}
            setSort={setSort}
            onRole={(member, role) => mutation.mutate({ operation: 'update', userId: member.user_id, role })}
            onRemove={(member) => {
              mutation.reset();
              setRemoving(member);
            }}
          />
        </>
      )}
      {adding ? (
        <AddWorkspaceMemberDialog
          orgUuid={orgUuid}
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
        title="Remove from workspace?"
        pending={mutation.isPending}
        error={mutation.error?.message}
        onClose={() => setRemoving(null)}
        onConfirm={() => {
          if (removing)
            mutation.mutate({ operation: 'delete', userId: removing.user_id, role: removing.workspace_role });
        }}
      >
        {removing?.email} will lose access to this workspace. Their organization membership, other workspaces and this
        workspace’s team resources will remain.
      </MemberRemovalDialog>
    </section>
  );
}

function WorkspaceMemberHeading({ directory, onAdd }: { directory?: WorkspaceMemberDirectory; onAdd: () => void }) {
  return (
    <>
      <div className="mb-2 flex items-center justify-between gap-4">
        <h1 className="flex items-center gap-2 text-xl font-semibold">
          Members <Badge variant="secondary">{directory?.members.length ?? 0}</Badge>
        </h1>
        {directory?.can_manage_members && !directory.is_default ? (
          <Button onClick={onAdd}>
            <Plus className="size-4" aria-hidden />
            Add to Workspace
          </Button>
        ) : null}
      </div>
      <p className="mb-4 text-sm text-muted-foreground">
        {directory?.is_default
          ? 'Default Workspace members and permissions are inherited from organization roles.'
          : 'Organization admins inherit the workspace admin role. Billing members inherit the workspace billing role and can be promoted to workspace admin.'}
      </p>
    </>
  );
}
