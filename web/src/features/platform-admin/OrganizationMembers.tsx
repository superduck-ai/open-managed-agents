import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Badge } from '../../shared/ui/badge';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { OrganizationMemberList } from '../settings/OrganizationMemberList';
import { OrganizationMemberRemoval } from '../settings/OrganizationMemberRemoval';
import type { OrganizationMember } from '../settings/membersApi';
import type { PlatformRole } from '../../shared/permissions/roles';
import { usePlatformAdmin } from './context';
import { usePlatformAdminError } from './usePlatformAdminError';
import { AddMemberDialog } from './AddMemberDialog';
import type { Organization, Page } from './types';

type Member = { uuid: string; name: string; email: string; role: PlatformRole };
export function OrganizationMembers({
  organization,
  onChanged,
}: {
  organization: Organization;
  onChanged: (organization: Organization) => void;
}) {
  const { msg } = useI18n(),
    { client } = usePlatformAdmin(),
    errorMessage = usePlatformAdminError(),
    queries = useQueryClient();
  const [search, setSearch] = useState(''),
    [role, setRole] = useState('all'),
    [cursors, setCursors] = useState(['']),
    [adding, setAdding] = useState(false);
  const cursor = cursors.at(-1) ?? '',
    base = `/organizations/${organization.uuid}`;
  const key = ['platform-admin', 'members', organization.uuid, search, role, cursor];
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) =>
      client.request<Page<Member>>(
        `${base}/members?${new URLSearchParams({ query: search, role: role === 'all' ? '' : role, cursor })}`,
        { signal },
      ),
  });
  async function refresh() {
    await Promise.all([
      queries.invalidateQueries({ queryKey: ['platform-admin', 'members', organization.uuid] }),
      queries.invalidateQueries({ queryKey: ['platform-admin', 'organization', organization.uuid] }),
      queries.invalidateQueries({ queryKey: ['platform-admin', 'organizations'] }),
    ]);
  }
  async function change(method: string, member: OrganizationMember, role?: PlatformRole) {
    try {
      const updated = await client.request<Organization>(`${base}/members/${member.id}`, {
        method,
        body: { expected_version: organization.version, ...(role ? { role } : {}) },
      });
      onChanged(updated);
      await refresh();
    } catch (error) {
      if ((error as { code?: string }).code === 'version_conflict') await refresh();
      throw error;
    }
  }
  const update = useMutation({
    mutationFn: ({ member, role }: { member: OrganizationMember; role: PlatformRole }) => change('PATCH', member, role),
  });
  const rows = (query.data?.data ?? []).map((member) => ({ ...member, id: member.uuid, type: 'user' as const }));
  return (
    <section>
      <ResourcePageHeader
        title={msg('members.title', 'Members')}
        contentGap="content"
        titleAdornment={
          <Badge variant="secondary" className="rounded-full">
            {organization.member_count}
          </Badge>
        }
        actions={
          <Button size="lg" onClick={() => setAdding(true)}>
            <Plus className="size-4" />
            {msg('platformAdmin.addMember', 'Add member')}
          </Button>
        }
      />
      {update.isError && (
        <p role="alert" className="mb-4 text-destructive">
          {errorMessage(update.error)}
        </p>
      )}
      <OrganizationMemberList
        rows={rows}
        canManage
        canInvite={false}
        canEdit={() => true}
        onRole={(member, role) => update.mutate({ member, role })}
        rolePending={update.isPending}
        loading={query.isPending}
        hasError={query.isError}
        filtered={Boolean(search) || role !== 'all'}
        onRetry={() => void query.refetch()}
        search={search}
        onSearch={(value) => {
          setSearch(value);
          setCursors(['']);
        }}
        role={role}
        onFilterRole={(value) => {
          setRole(value);
          setCursors(['']);
        }}
        renderActions={(row) => (
          <OrganizationMemberRemoval
            orgUuid={organization.uuid}
            organizationName={organization.name}
            member={row as OrganizationMember}
            disabled={update.isPending}
            remove={() => change('DELETE', row as OrganizationMember)}
            onRemoved={async () => {
              setCursors(['']);
              await refresh();
            }}
            formatError={errorMessage}
          />
        )}
      />
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="outline" disabled={cursors.length < 2} onClick={() => setCursors(cursors.slice(0, -1))}>
          {msg('platformAdmin.previous', 'Previous')}
        </Button>
        <Button
          variant="outline"
          disabled={!query.data?.next_cursor}
          onClick={() => setCursors([...cursors, query.data!.next_cursor])}
        >
          {msg('platformAdmin.next', 'Next')}
        </Button>
      </div>
      {adding && (
        <AddMemberDialog
          organization={organization}
          onClose={() => setAdding(false)}
          onSaved={(updated) => {
            onChanged(updated);
            setAdding(false);
            setCursors(['']);
            void refresh();
          }}
        />
      )}
    </section>
  );
}
