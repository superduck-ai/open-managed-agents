import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { AccountPicker, type RegisteredAccount } from './AccountPicker';
import { usePlatformAdmin } from './context';
import { usePlatformAdminError } from './usePlatformAdminError';

export function SuperAdministratorDialog({
  account,
  onClose,
  onSaved,
}: {
  account?: RegisteredAccount;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const { client, bootstrap } = usePlatformAdmin();
  const errorMessage = usePlatformAdminError();
  const [selected, setSelected] = useState<RegisteredAccount[]>([]);
  const mutation = useMutation({
    mutationFn: () =>
      account
        ? client.request(`/administrators/${account.uuid}`, { method: 'DELETE' })
        : client.request('/administrators', { method: 'POST', body: { account_uuid: selected[0].uuid } }),
    onSuccess: onSaved,
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose();
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl" showCloseButton={!mutation.isPending}>
        <DialogHeader>
          <DialogTitle>
            {account
              ? msg('platformAdmin.revokeSuperAdministrator', 'Revoke super administrator')
              : msg('platformAdmin.addSuperAdministrator', 'Add super administrator')}
          </DialogTitle>
          <DialogDescription>
            {account
              ? msg(
                  'platformAdmin.revokeSuperAdministratorDescription',
                  'Revoke platform access for {email}. Their organization roles remain unchanged. At least one effective super administrator must remain.',
                  { email: account.email },
                )
              : msg(
                  'platformAdmin.addSuperAdministratorDescription',
                  'Select a registered account to grant access to all organizations and super administrator management.',
                )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if ((account || selected.length === 1) && !mutation.isPending) mutation.mutate();
          }}
        >
          {!account && (
            <AccountPicker selected={selected} onChange={setSelected} limit={1} disabled={mutation.isPending} />
          )}
          {account?.uuid === bootstrap.account.uuid && (
            <p className="text-sm text-destructive">
              {msg(
                'platformAdmin.revokingOwnPlatformAccess',
                'You are revoking your own platform access. You will leave administration after confirmation.',
              )}
            </p>
          )}
          {mutation.isError && (
            <p role="alert" className="text-destructive">
              {errorMessage(mutation.error)}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>
              {msg('platformAdmin.cancel', 'Cancel')}
            </Button>
            <Button
              type="submit"
              variant={account ? 'destructive' : 'default'}
              disabled={(!account && selected.length !== 1) || mutation.isPending}
            >
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
