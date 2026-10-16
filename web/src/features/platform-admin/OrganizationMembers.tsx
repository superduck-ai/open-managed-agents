import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { Button } from '../../shared/ui/button';
import { AdministratorDialog, type AdministratorAction, type Member } from './AdministratorDialog';
import { DirectoryTable } from './DirectoryTable';
import type { Organization } from './types';

export function OrganizationMembers({
  organization,
  onChanged,
}: {
  organization: Organization;
  onChanged: (organization: Organization) => void;
}) {
  const { msg } = useI18n();

  const queries = useQueryClient();
  const [action, setAction] = useState<AdministratorAction | null>(null);
  const columns: ColumnDef<Member>[] = [
    { accessorKey: 'name', header: msg('platformAdmin.name', 'Name') },
    { accessorKey: 'email', header: msg('platformAdmin.email', 'Email') },
    { accessorKey: 'role', header: msg('platformAdmin.organizationRole', 'Organization role') },
    {
      id: 'actions',
      header: msg('platformAdmin.administratorActions', 'Administrator actions'),
      cell: ({ row }) =>
        row.original.role === 'admin' ? (
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => setAction({ kind: 'transfer', member: row.original })}>
              {msg('platformAdmin.transfer', 'Transfer')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={organization.admin_count <= 1}
              title={
                organization.admin_count <= 1
                  ? msg(
                      'platformAdmin.atLeastOneAdministratorMustRemainUseTransferInstead',
                      'At least one administrator must remain. Use transfer instead.',
                    )
                  : undefined
              }
              onClick={() => setAction({ kind: 'change-role', member: row.original })}
            >
              {msg('platformAdmin.changeRole', 'Change role')}
            </Button>
          </div>
        ) : null,
    },
  ];
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">{msg('platformAdmin.organizationMembers', 'Organization members')}</h2>
        <Button variant="outline" onClick={() => setAction({ kind: 'grant' })}>
          {msg('platformAdmin.assignAdministrator', 'Assign administrator')}
        </Button>
      </div>
      <DirectoryTable
        path={`/organizations/${organization.uuid}/members`}
        columns={columns}
        label={msg('platformAdmin.organizationMembers', 'Organization members')}
      />
      {action && (
        <AdministratorDialog
          organization={organization}
          action={action}
          onClose={() => setAction(null)}
          onSaved={(updated) => {
            setAction(null);
            onChanged(updated);
            void queries.invalidateQueries({ queryKey: ['platform-admin', 'directory'] });
          }}
        />
      )}
    </section>
  );
}
