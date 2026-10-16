import { accountRoleLabel } from '../../shared/permissions/roles';
import { useFormatters } from '../../shared/i18n';
import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { Button } from '../../shared/ui/button';
import { DirectoryTable } from './DirectoryTable';
import { InvitationDialog, type OrganizationInvitation, type InviteAction } from './InvitationDialog';
import type { Organization } from './types';

export function OrganizationInvitations({
  organization,
  onChanged,
}: {
  organization: Organization;
  onChanged: (organization: Organization) => void;
}) {
  const { msg } = useI18n();
  const format = useFormatters();

  const statuses: Record<string, string> = {
    pending: msg('platformAdmin.pending', 'Pending'),
    accepted: msg('platformAdmin.accepted', 'Accepted'),
    expired: msg('platformAdmin.expired', 'Expired'),
    declined: msg('platformAdmin.declined', 'Declined'),
    deleted: msg('platformAdmin.revoked', 'Revoked'),
  };
  const deliveries: Record<string, string> = {
    pending: msg('platformAdmin.queued', 'Queued'),
    sending: msg('platformAdmin.sending', 'Sending'),
    failed: msg('platformAdmin.deliveryFailed', 'Delivery failed'),
    sent: msg('platformAdmin.acceptedBySMTP', 'Accepted by SMTP'),
    cancelled: msg('platformAdmin.canceled', 'Canceled'),
    not_queued: msg('platformAdmin.noPlatformDeliveryRecord', 'No platform delivery record'),
  };

  const queries = useQueryClient();
  const [action, setAction] = useState<InviteAction | null>(null);
  const columns: ColumnDef<OrganizationInvitation>[] = [
    { accessorKey: 'email', header: msg('platformAdmin.inviteeEmail', 'Invitee email') },
    {
      accessorKey: 'role',
      header: msg('platformAdmin.organizationRole', 'Organization role'),
      cell: ({ row }) => accountRoleLabel(row.original.role, msg),
    },
    {
      accessorKey: 'status',
      header: msg('platformAdmin.invitationStatus', 'Invitation status'),
      cell: ({ row }) => statuses[row.original.status] ?? row.original.status,
    },
    {
      accessorKey: 'delivery_status',
      header: msg('platformAdmin.emailStatus', 'Email status'),
      cell: ({ row }) => deliveries[row.original.delivery_status] ?? row.original.delivery_status,
    },
    {
      accessorKey: 'expires_at',
      header: msg('platformAdmin.expiresAt', 'Expires at'),
      cell: ({ row }) => format.date(row.original.expires_at, { dateStyle: 'medium', timeStyle: 'short' }),
    },
    {
      id: 'actions',
      header: msg('platformAdmin.actions', 'Actions'),
      cell: ({ row }) =>
        ['pending', 'expired'].includes(row.original.status) ? (
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => setAction({ kind: 'resend', invitation: row.original })}>
              {msg('platformAdmin.resend', 'Resend')}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setAction({ kind: 'revoke', invitation: row.original })}>
              {msg('platformAdmin.revoke', 'Revoke')}
            </Button>
          </div>
        ) : null,
    },
  ];
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">{msg('platformAdmin.memberInvitations', 'Member invitations')}</h2>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() =>
              void queries.invalidateQueries({
                queryKey: ['platform-admin', 'directory', `/organizations/${organization.uuid}/invitations`],
              })
            }
          >
            {msg('platformAdmin.refreshStatus', 'Refresh status')}
          </Button>
          <Button variant="outline" onClick={() => setAction({ kind: 'create' })}>
            {msg('platformAdmin.sendInvitation', 'Send invitation')}
          </Button>
        </div>
      </div>
      <p className="text-sm text-muted-foreground">
        {msg(
          'platformAdmin.sMTPAcceptanceDoesNotConfirmInboxDeliveryOrInvitation',
          'SMTP acceptance does not confirm inbox delivery or invitation acceptance.',
        )}
      </p>
      <DirectoryTable
        path={`/organizations/${organization.uuid}/invitations`}
        columns={columns}
        label={msg('platformAdmin.memberInvitations', 'Member invitations')}
      />
      {action && (
        <InvitationDialog
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
