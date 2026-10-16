import { usePlatformAdminError } from './usePlatformAdminError';
import { useI18n } from '../../shared/i18n';
import { useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { AccountPicker, type RegisteredAccount } from './AccountPicker';
import { usePlatformAdmin } from './context';
import type { Organization } from './types';

export function CreateOrganizationDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (organization: Organization) => void;
}) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();

  const { client } = usePlatformAdmin();
  const [name, setName] = useState('');
  const [accounts, setAccounts] = useState<RegisteredAccount[]>([]);
  const submission = useRef({ body: '', key: '' });
  const mutation = useMutation({
    mutationFn: async () => {
      const body = {
        name: name.trim(),
        admin_account_uuids: accounts.map((account) => account.uuid).sort(),
      };
      const encoded = JSON.stringify(body);
      if (submission.current.body !== encoded) submission.current = { body: encoded, key: crypto.randomUUID() };
      return client.request<Organization>('/organizations', { method: 'POST', body, key: submission.current.key });
    },
    onSuccess: onCreated,
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !mutation.isPending) onClose();
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl" showCloseButton={!mutation.isPending}>
        <DialogHeader>
          <DialogTitle>{msg('platformAdmin.createOrganization', 'Create organization')}</DialogTitle>
          <DialogDescription>
            {msg(
              'platformAdmin.chooseAtLeastOneRegisteredAdministratorADefaultWorkspace',
              'Choose at least one registered administrator. A Default workspace will be created.',
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (accounts.length && !mutation.isPending) mutation.mutate();
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="platform-org-name">{msg('platformAdmin.organizationName', 'Organization name')}</Label>
            <Input
              id="platform-org-name"
              required
              maxLength={200}
              value={name}
              disabled={mutation.isPending}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <fieldset className="space-y-2">
            <legend className="mb-2 font-medium">
              {msg('platformAdmin.initialAdministratorsCount20', 'Initial administrators ({count}/20)', {
                count: accounts.length,
              })}
            </legend>
            <AccountPicker selected={accounts} onChange={setAccounts} disabled={mutation.isPending} />
          </fieldset>
          {mutation.isError && (
            <p role="alert" className="text-destructive">
              {errorMessage(mutation.error) ||
                msg('platformAdmin.creationFailedPleaseTryAgain', 'Creation failed. Please try again.')}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>
              {msg('platformAdmin.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={mutation.isPending || !accounts.length || !name.trim()}>
              {mutation.isPending
                ? msg('platformAdmin.creating', 'Creating…')
                : msg('platformAdmin.createOrganization', 'Create organization')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
