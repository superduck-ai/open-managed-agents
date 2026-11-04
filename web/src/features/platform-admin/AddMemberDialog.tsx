import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogClose } from '../../shared/ui/dialog';
import { Label } from '../../shared/ui/label';
import { RoleSelect } from '../../shared/permissions/RoleSelect';
import type { PlatformRole } from '../../shared/permissions/roles';
import { useDialogDismissal } from '../../shared/hooks/use-dialog-dismissal';
import { AccountPicker, type RegisteredAccount } from './AccountPicker';
import { usePlatformAdmin } from './context';
import { usePlatformAdminError } from './usePlatformAdminError';
import type { Organization } from './types';
export function AddMemberDialog({
  organization,
  onClose,
  onSaved,
}: {
  organization: Organization;
  onClose: () => void;
  onSaved: (organization: Organization) => void;
}) {
  const { msg } = useI18n(),
    { client } = usePlatformAdmin(),
    errorMessage = usePlatformAdminError(),
    queries = useQueryClient();
  const [snapshot] = useState(organization),
    [accounts, setAccounts] = useState<RegisteredAccount[]>([]),
    [role, setRole] = useState<PlatformRole>('user'),
    [saved, setSaved] = useState<Organization | null>(null);
  const mutation = useMutation({
    mutationFn: () =>
      client.request<Organization>(`/organizations/${snapshot.uuid}/members`, {
        method: 'POST',
        body: { account_uuid: accounts[0].uuid, role, expected_version: snapshot.version },
      }),
    onSuccess: setSaved,
    onError: async (error) => {
      if ((error as { code?: string }).code === 'version_conflict')
        await Promise.all([
          queries.invalidateQueries({ queryKey: ['platform-admin', 'organization', snapshot.uuid] }),
          queries.invalidateQueries({ queryKey: ['platform-admin', 'members', snapshot.uuid] }),
        ]);
    },
  });
  const dialog = useDialogDismissal(() => {
    if (saved) onSaved(saved);
    else onClose();
  }, mutation.isPending);
  return (
    <Dialog {...dialog} open={dialog.open && !saved}>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-xl"
        closeButtonDisabled={mutation.isPending}
      >
        <DialogHeader>
          <DialogTitle>{msg('platformAdmin.addMember', 'Add member')}</DialogTitle>
        </DialogHeader>
        <form
          className="flex min-h-0 flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (accounts.length === 1 && !mutation.isPending) mutation.mutate();
          }}
        >
          <div className="min-h-0 space-y-4 overflow-y-auto">
            <div className="space-y-2">
              <Label>{msg('platformAdmin.account', 'Account')}</Label>
              <AccountPicker selected={accounts} onChange={setAccounts} disabled={mutation.isPending} limit={1} />
            </div>
            <div className="space-y-2">
              <Label>{msg('platformAdmin.organizationRole', 'Organization role')}</Label>
              <RoleSelect
                value={role}
                onChange={setRole}
                includeAdmin
                disabled={mutation.isPending}
                ariaLabel={msg('platformAdmin.organizationRole', 'Organization role')}
                className="w-full"
              />
            </div>
            {mutation.isError && (
              <p role="alert" className="text-destructive">
                {errorMessage(mutation.error)}
              </p>
            )}
          </div>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={mutation.isPending} />}>
              {msg('platformAdmin.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={accounts.length !== 1 || mutation.isPending}>
              {mutation.isPending ? msg('platformAdmin.saving', 'Saving…') : msg('platformAdmin.add', 'Add')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
