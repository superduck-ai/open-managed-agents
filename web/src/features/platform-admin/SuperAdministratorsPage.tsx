import { useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import type { RegisteredAccount } from './AccountPicker';
import { DirectoryTable } from './DirectoryTable';
import { SuperAdministratorDialog } from './SuperAdministratorDialog';

export function SuperAdministratorsPage() {
  const { msg } = useI18n();
  const queries = useQueryClient();
  const finalFocus = useRef<HTMLElement | null>(null);
  const [action, setAction] = useState<{ account?: RegisteredAccount } | null>(null);
  const title = msg('platformAdmin.superAdministrators', 'Super administrators');
  const columns: ColumnDef<RegisteredAccount>[] = [
    { accessorKey: 'name', header: msg('platformAdmin.name', 'Name') },
    { accessorKey: 'email', header: msg('platformAdmin.email', 'Email') },
    { accessorKey: 'uuid', header: msg('platformAdmin.accountUUID', 'Account UUID') },
    {
      id: 'actions',
      header: msg('platformAdmin.actions', 'Actions'),
      cell: ({ row }) => (
        <Button
          variant="outline"
          size="sm"
          onClick={(event) => {
            finalFocus.current = event.currentTarget;
            setAction({ account: row.original });
          }}
        >
          {msg('platformAdmin.revokeSuperAdministrator', 'Revoke super administrator')}
        </Button>
      ),
    },
  ];
  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold">{title}</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {msg(
              'platformAdmin.superAdministratorsDescription',
              'Super administrators can manage all organizations and platform access. Organization administrator roles remain separate.',
            )}
          </p>
        </div>
        <Button
          className="shrink-0"
          onClick={(event) => {
            finalFocus.current = event.currentTarget;
            setAction({});
          }}
        >
          {msg('platformAdmin.addSuperAdministrator', 'Add super administrator')}
        </Button>
      </header>
      <DirectoryTable<RegisteredAccount> path="/administrators" columns={columns} label={title} />
      {action && (
        <SuperAdministratorDialog
          finalFocus={finalFocus}
          account={action.account}
          onClose={() => setAction(null)}
          onSaved={async () => {
            await queries.invalidateQueries({ queryKey: ['platform-admin', 'directory', '/administrators'] });
            await queries.invalidateQueries({ queryKey: ['platform-admin', 'bootstrap'] });
            setAction(null);
          }}
        />
      )}
    </section>
  );
}
