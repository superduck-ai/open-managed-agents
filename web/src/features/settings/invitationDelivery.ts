import { toast } from '../../shared/ui/sonner';
import { useI18n } from '../../shared/i18n';
import type { OrganizationInvite } from './membersApi';

type MsgFn = ReturnType<typeof useI18n>['msg'];

export function notifyInvitationDelivery(invites: OrganizationInvite[], msg: MsgFn) {
  const unsent = invites.filter((invite) => invite.email_delivery !== 'sent').length;
  if (unsent) {
    toast.warning(
      msg(
        'invitations.delivery.unconfirmed',
        `Invitations saved, but ${unsent} emails were not confirmed as sent. Check your mail configuration and resend the invitations.`,
        { count: unsent },
      ),
    );
  } else {
    toast.success(
      msg(
        'invitations.delivery.sent',
        'Invitation emails submitted for delivery. Ask recipients to check their inboxes.',
      ),
    );
  }
}
