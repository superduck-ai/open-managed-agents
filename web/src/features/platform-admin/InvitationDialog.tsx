import { usePlatformAdminError } from './usePlatformAdminError';
import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
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
  onSaved,
}: {
  organization: Organization;
  action: InviteAction;
  onClose: () => void;
  onSaved: (organization: Organization) => void;
}) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();

  const titles = {
    create: msg('platformAdmin.inviteOrganizationMember', 'Invite organization member'),
    resend: msg('platformAdmin.resendMemberInvitation', 'Resend member invitation'),
    revoke: msg('platformAdmin.revokeMemberInvitation', 'Revoke member invitation'),
  };
  const { client } = usePlatformAdmin();
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
    onSuccess: onSaved,
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose();
      }}
    >
      <DialogContent showCloseButton={!mutation.isPending}>
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
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (!mutation.isPending) mutation.mutate();
          }}
        >
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
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>
              {msg('platformAdmin.cancel', 'Cancel')}
            </Button>
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
