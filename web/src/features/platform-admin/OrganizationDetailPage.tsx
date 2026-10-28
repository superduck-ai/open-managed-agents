import { EnterOrganizationButton } from './EnterOrganizationButton';
import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { Link, useParams, useSearch } from '@tanstack/react-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { Button } from '../../shared/ui/button';
import { DirectoryTable } from './DirectoryTable';
import { OrganizationProfileForm } from './OrganizationProfileForm';
import { OrganizationMembers } from './OrganizationMembers';
import { OrganizationInvitations } from './OrganizationInvitations';
import { usePlatformAdmin } from './context';
import type { Organization } from './types';
import { organizationSection, type OrganizationSection } from './organizationNavigation';

type Workspace = { uuid: string; id: string; name: string; is_default: boolean; archived: boolean };

export function OrganizationDetailPage() {
  const { organizationUuid } = useParams({ from: '/platform-admin/organizations/$organizationUuid' });
  const search = useSearch({ from: '/platform-admin/organizations/$organizationUuid' });
  const view = organizationSection(search.view);
  return <OrganizationDetail key={`${organizationUuid}:${view}`} organizationUuid={organizationUuid} view={view} />;
}

function OrganizationDetail({ organizationUuid, view }: { organizationUuid: string; view: OrganizationSection }) {
  const { msg } = useI18n();

  const workspaceColumns: ColumnDef<Workspace>[] = [
    { accessorKey: 'name', header: msg('platformAdmin.workspaces', 'Workspaces') },
    { accessorKey: 'id', header: msg('platformAdmin.workspaceID', 'Workspace ID') },
    {
      id: 'status',
      header: msg('platformAdmin.status', 'Status'),
      cell: ({ row }) =>
        row.original.archived
          ? msg('platformAdmin.archived', 'Archived')
          : row.original.is_default
            ? msg('platformAdmin.defaultWorkspace', 'Default')
            : msg('platformAdmin.active', 'Active'),
    },
  ];

  const { client } = usePlatformAdmin();
  const queries = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState('');
  const key = ['platform-admin', 'organization', organizationUuid];
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => client.request<Organization>(`/organizations/${organizationUuid}`, { signal }),
  });
  if (query.isPending)
    return <p role="status">{msg('platformAdmin.loadingOrganizationDetails', 'Loading organization details…')}</p>;
  if (query.isError)
    return (
      <div role="alert">
        {msg(
          'platformAdmin.theOrganizationDoesNotExistOrCannotBeLoaded',
          'The organization does not exist or cannot be loaded right now.',
        )}
        <Button variant="outline" onClick={() => void query.refetch()}>
          {msg('platformAdmin.retry', 'Retry')}
        </Button>
        <Link to="/platform-admin">{msg('platformAdmin.backToOrganizations', 'Back to organizations')}</Link>
      </div>
    );
  const organization = query.data;
  return (
    <section className="space-y-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-3">
          <h1 className="text-2xl font-semibold [overflow-wrap:anywhere]">{organization.name}</h1>
          <p className="break-all text-sm text-muted-foreground">{organization.uuid}</p>
          <dl className="flex flex-wrap gap-x-4 gap-y-2 text-sm">
            {[
              [msg('platformAdmin.administrators', 'Administrators'), organization.admin_count],
              [msg('platformAdmin.members', 'Members'), organization.member_count],
              [msg('platformAdmin.workspaces', 'Workspaces'), organization.workspace_count],
            ].map(([label, count]) => (
              <div key={label} className="flex gap-2">
                <dt className="text-muted-foreground">{label}</dt>
                <dd>{count}</dd>
              </div>
            ))}
          </dl>
        </div>
        <div className="flex flex-wrap gap-2">
          <EnterOrganizationButton organizationUuid={organizationUuid} />
          {view === 'profile' && (
            <Button className="shrink-0" variant="outline" onClick={() => setEditing(true)} disabled={editing}>
              {msg('platformAdmin.editDetails', 'Edit details')}
            </Button>
          )}
        </div>
      </header>
      {notice && <p role="status">{msg(notice, '')}</p>}
      {view === 'profile' &&
        (editing ? (
          <OrganizationProfileForm
            organization={organization}
            onCancel={() => {
              setEditing(false);
              void query.refetch();
            }}
            onSaved={(updated) => {
              queries.setQueryData(key, updated);
              setEditing(false);
              setNotice('platformAdmin.organizationDetailsSaved');
              void queries.invalidateQueries({ queryKey: ['platform-admin', 'organizations'] });
            }}
          />
        ) : (
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">{msg('platformAdmin.website', 'Website')}</dt>
              <dd>{organization.website || msg('platformAdmin.notProvided', 'Not provided')}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{msg('platformAdmin.industry', 'Industry')}</dt>
              <dd>{organization.industry || msg('platformAdmin.notProvided', 'Not provided')}</dd>
            </div>
          </dl>
        ))}
      {view === 'members' && (
        <OrganizationMembers
          organization={organization}
          onChanged={(updated) => {
            queries.setQueryData(key, updated);
            setNotice('platformAdmin.organizationAdministratorsUpdated');
            void queries.invalidateQueries({ queryKey: ['platform-admin', 'organizations'] });
          }}
        />
      )}
      {view === 'workspaces' && (
        <section className="space-y-3">
          <h2 className="text-lg font-semibold">{msg('platformAdmin.workspaceDirectory', 'Workspace directory')}</h2>
          <DirectoryTable
            path={`/organizations/${organizationUuid}/workspaces`}
            columns={workspaceColumns}
            label={msg('platformAdmin.workspaces', 'Workspaces')}
          />
        </section>
      )}
      {view === 'invitations' && (
        <OrganizationInvitations
          organization={organization}
          onChanged={(updated) => {
            queries.setQueryData(key, updated);
            setNotice('platformAdmin.memberInvitationsUpdated');
          }}
        />
      )}
    </section>
  );
}
