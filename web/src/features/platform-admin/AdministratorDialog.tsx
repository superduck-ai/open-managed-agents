import { usePlatformAdminError } from './usePlatformAdminError';
import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Label } from '../../shared/ui/label';
import { OrganizationRoleSelect } from './OrganizationRoleSelect';
import { AccountPicker, type RegisteredAccount } from './AccountPicker';
import { usePlatformAdmin } from './context';
import type { Organization } from './types';

export type Member = { uuid: string; email: string; name: string; role: string };
export type AdministratorAction = { kind: 'grant' } | { kind: 'change-role' | 'transfer'; member: Member };

export function AdministratorDialog({
  organization,
  action,
  onClose,
  onSaved,
}: {
  organization: Organization;
  action: AdministratorAction;
  onClose: () => void;
  onSaved: (organization: Organization) => void;
}) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();

  const titles = {
    grant: msg('platformAdmin.assignOrganizationAdministrator', 'Assign organization administrator'),
    'change-role': msg('platformAdmin.changeRole', 'Change role'),
    transfer: msg('platformAdmin.transferOrganizationAdministration', 'Transfer organization administration'),
  };

  const { client } = usePlatformAdmin();
  const [snapshot] = useState(organization);
  const [accounts, setAccounts] = useState<RegisteredAccount[]>([]);
  const [role, setRole] = useState(action.kind === 'change-role' ? action.member.role : 'user');
  const mutation = useMutation({
    mutationFn: () => {
      const change = { expected_version: snapshot.version };
      const base = `/organizations/${snapshot.uuid}`;
      if (action.kind === 'change-role')
        return client.request<Organization>(`${base}/administrators/${action.member.uuid}`, {
          method: 'PATCH',
          body: { ...change, role },
        });
      if (action.kind === 'transfer')
        return client.request<Organization>(`${base}/administrator-transfers`, {
          method: 'POST',
          body: { ...change, account_uuid: accounts[0].uuid, previous_member_uuid: action.member.uuid, role },
        });
      return client.request<Organization>(`${base}/administrators`, {
        method: 'POST',
        body: { ...change, account_uuid: accounts[0].uuid },
      });
    },
    onSuccess: onSaved,
  });
  const valid = action.kind === 'change-role' ? role !== action.member.role && role !== 'admin' : accounts.length === 1;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose();
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl" showCloseButton={!mutation.isPending}>
        <DialogHeader>
          <DialogTitle>{titles[action.kind]}</DialogTitle>
          <DialogDescription>
            {action.kind === 'grant'
              ? msg(
                  'platformAdmin.grantARegisteredAccountAdministratorAccessToThisOrganization',
                  'Grant a registered account administrator access to this organization.',
                )
              : action.kind === 'change-role'
                ? msg(
                    'platformAdmin.currentAdministratorEmailTheSelectedRoleWillApplyAfter',
                    'Current administrator: {email}. The selected role will apply after this change. At least one administrator must remain. The previous administrator may lose inherited workspace access.',
                    { email: action.member.email },
                  )
                : msg(
                    'platformAdmin.currentAdministratorEmailTheTransferGrantsTheNewAdministrator',
                    'Current administrator: {email}. The transfer grants the new administrator access and changes the previous administrator’s role atomically. The previous administrator may lose inherited workspace access.',
                    { email: action.member.email },
                  )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (valid && !mutation.isPending) mutation.mutate();
          }}
        >
          {action.kind !== 'change-role' && (
            <AccountPicker selected={accounts} onChange={setAccounts} limit={1} disabled={mutation.isPending} />
          )}
          {action.kind !== 'grant' && (
            <div className="space-y-2">
              <Label>
                {action.kind === 'change-role'
                  ? msg('platformAdmin.organizationRole', 'Organization role')
                  : msg('platformAdmin.previousAdministratorSNewRole', 'Previous administrator’s new role')}
              </Label>
              <OrganizationRoleSelect
                value={role}
                onChange={setRole}
                disabled={mutation.isPending}
                includeAdmin={action.kind === 'change-role'}
                label={
                  action.kind === 'change-role'
                    ? msg('platformAdmin.organizationRole', 'Organization role')
                    : msg('platformAdmin.previousAdministratorSNewRole', 'Previous administrator’s new role')
                }
              />
            </div>
          )}
          {mutation.isError && (
            <p role="alert" className="text-destructive">
              {errorMessage(mutation.error)}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={mutation.isPending}>
              {msg('platformAdmin.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={!valid || mutation.isPending}>
              {mutation.isPending
                ? msg('platformAdmin.saving', 'Saving…')
                : msg('platformAdmin.confirmChange', 'Confirm change')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
