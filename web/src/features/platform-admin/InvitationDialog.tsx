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
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { RoleSelect } from '../../shared/permissions/RoleSelect';
import type { PlatformRole } from '../../shared/permissions/roles';
import { usePlatformAdmin } from './context';
import type { Organization } from './types';

export type OrganizationInvitation = {
  uuid: string;
  id: string;
  email: string;
  role: string;
  status: string;
  invited_at: string;
  expires_at: string;
  delivery_status: string;
};
export type InviteAction = { kind: 'create' } | { kind: 'resend' | 'revoke'; invitation: OrganizationInvitation };

export function InvitationDialog({
  organization,
  action,
  onClose,
  finalFocus,
  onSaved,
}: {
  organization: Organization;
  action: InviteAction;
  onClose: () => void;
  finalFocus?: RefObject<HTMLElement | null>;
  onSaved: (organization: Organization) => void;
}) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();
  const [closing, setClosing] = useState(false);
  const saved = useRef<Organization | null>(null);

  const titles = {
    create: msg('platformAdmin.inviteOrganizationMember', 'Invite organization member'),
    resend: msg('platformAdmin.resendMemberInvitation', 'Resend member invitation'),
    revoke: msg('platformAdmin.revokeMemberInvitation', 'Revoke member invitation'),
  };
  const { client } = usePlatformAdmin();
  const queries = useQueryClient();
  const [snapshot] = useState(organization);
  const [email, setEmail] = useState(action.kind === 'create' ? '' : action.invitation.email);
  const [role, setRole] = useState<PlatformRole>(
    action.kind === 'create' ? 'user' : action.invitation.role === 'admin' ? 'admin' : 'user',
  );
  const mutation = useMutation({
    mutationFn: () => {
      const base = `/organizations/${snapshot.uuid}/invitations`;
      const change = { expected_version: snapshot.version };
      return action.kind === 'create'
        ? client.request<Organization>(base, { method: 'POST', body: { ...change, email: email.trim(), role } })
        : client.request<Organization>(`${base}/${action.invitation.uuid}/${action.kind}`, {
            method: 'POST',
            body: change,
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
            queryKey: ['platform-admin', 'directory', `/organizations/${snapshot.uuid}/invitations`],
          }),
        ]);
      }
    },
  });
  const dialogProps = useDialogDismissal(() => {
    if (saved.current) onSaved(saved.current);
    else onClose();
  }, mutation.isPending);
  return (
    <Dialog {...dialogProps} open={dialogProps.open && !closing}>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden"
        finalFocus={finalFocus}
        closeButtonDisabled={mutation.isPending}
      >
        <DialogHeader>
          <DialogTitle>{titles[action.kind]}</DialogTitle>
          <DialogDescription>
            {action.kind === 'revoke'
              ? msg(
                  'platformAdmin.theInvitationCanNoLongerBeAcceptedAfterRevocation',
                  'The invitation can no longer be accepted after revocation. Pending email deliveries will be canceled.',
                )
              : msg(
                  'platformAdmin.theRecipientMustSignInWithThisEmailAnd',
                  'The recipient must sign in with this email and accept the invitation to join with the selected role. Manage existing members from the members page.',
                )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex min-h-0 flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (!mutation.isPending) mutation.mutate();
          }}
        >
          <div className="min-h-0 space-y-4 overflow-y-auto [overflow-wrap:anywhere]">
            <div className="space-y-2">
              <Label htmlFor="platform-invite-email">{msg('platformAdmin.inviteeEmail', 'Invitee email')}</Label>
              <Input
                id="platform-invite-email"
                type="email"
                required
                maxLength={254}
                value={email}
                disabled={action.kind !== 'create' || mutation.isPending}
                onChange={(event) => setEmail(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>{msg('platformAdmin.organizationRole', 'Organization role')}</Label>
              <RoleSelect
                className="w-full"
                contentClassName="w-[min(26rem,calc(100vw-3rem))]"
                value={role}
                onChange={setRole}
                ariaLabel={msg('platformAdmin.organizationRole', 'Organization role')}
                disabled={action.kind !== 'create' || mutation.isPending}
              />
            </div>
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
            <Button type="submit" disabled={mutation.isPending || !email.trim()}>
              {mutation.isPending
                ? msg('platformAdmin.submitting', 'Submitting…')
                : msg('platformAdmin.confirm', 'Confirm')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
