import { useDialogDismissal } from '../../shared/hooks/use-dialog-dismissal';
import { usePlatformAdminError } from './usePlatformAdminError';
import { useI18n } from '../../shared/i18n';
import { useRef, useState, type RefObject } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { ApiError } from '../../shared/api/client';
import { Button } from '../../shared/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Label } from '../../shared/ui/label';
import { RoleSelect } from '../../shared/permissions/RoleSelect';
import type { PlatformRole } from '../../shared/permissions/roles';
import { AccountPicker, type RegisteredAccount } from './AccountPicker';
import { usePlatformAdmin } from './context';
import type { Organization } from './types';

export type Member = { uuid: string; email: string; name: string; role: string };
export type AdministratorAction = { kind: 'grant' } | { kind: 'change-role' | 'transfer'; member: Member };

export function AdministratorDialog({
  organization,
  action,
  onClose,
  finalFocus,
  onSaved,
}: {
  organization: Organization;
  action: AdministratorAction;
  onClose: () => void;
  finalFocus?: RefObject<HTMLElement | null>;
  onSaved: (organization: Organization) => void;
}) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();
  const [closing, setClosing] = useState(false);
  const saved = useRef<Organization | null>(null);

  const titles = {
    grant: msg('platformAdmin.assignOrganizationAdministrator', 'Assign organization administrator'),
    'change-role': msg('platformAdmin.changeRole', 'Change role'),
    transfer: msg('platformAdmin.transferOrganizationAdministration', 'Transfer organization administration'),
  };

  const { client } = usePlatformAdmin();
  const queries = useQueryClient();
  const [snapshot] = useState(organization);
  const [accounts, setAccounts] = useState<RegisteredAccount[]>([]);
  const [role, setRole] = useState<PlatformRole>(
    action.kind === 'change-role' ? (action.member.role === 'admin' ? 'admin' : 'user') : 'user',
  );
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
    onSuccess: (result) => {
      saved.current = result;
      setClosing(true);
    },
    onError: async (error) => {
      if ((error as unknown as ApiError).code === 'version_conflict') {
        await Promise.all([
          queries.invalidateQueries({ queryKey: ['platform-admin', 'organization', snapshot.uuid] }),
          queries.invalidateQueries({
            queryKey: ['platform-admin', 'directory', `/organizations/${snapshot.uuid}/members`],
          }),
        ]);
      }
    },
  });
  const valid = action.kind === 'change-role' ? role !== action.member.role && role !== 'admin' : accounts.length === 1;
  const dialogProps = useDialogDismissal(() => {
    if (saved.current) onSaved(saved.current);
    else onClose();
  }, mutation.isPending);
  return (
    <Dialog {...dialogProps} open={dialogProps.open && !closing}>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-xl"
        finalFocus={finalFocus}
        closeButtonDisabled={mutation.isPending}
      >
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
          className="flex min-h-0 flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (valid && !mutation.isPending) mutation.mutate();
          }}
        >
          <div className="min-h-0 space-y-4 overflow-y-auto [overflow-wrap:anywhere]">
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
                <RoleSelect
                  className="w-full"
                  contentClassName="w-[min(26rem,calc(100vw-3rem))]"
                  value={role}
                  onChange={setRole}
                  disabled={mutation.isPending}
                  includeAdmin={action.kind === 'change-role'}
                  ariaLabel={
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
          </div>
          <div className="flex justify-end gap-2">
            <DialogClose render={<Button type="button" variant="outline" disabled={mutation.isPending} />}>
              {msg('platformAdmin.cancel', 'Cancel')}
            </DialogClose>
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
