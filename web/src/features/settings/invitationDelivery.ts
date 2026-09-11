import { toast } from '../../shared/ui/sonner';
import type { OrganizationInvite } from './membersApi';
import type { I18nContextValue } from '../../shared/i18n/context';

export function notifyInvitationDelivery(invites: OrganizationInvite[], msg: I18nContextValue['msg']) {
  const unsent = invites.filter((invite) => invite.email_delivery !== 'sent').length;
  if (unsent) {
    toast.warning(
      msg(
        'invitations.delivery.unconfirmed',
        'Invitations saved, but delivery of {count, plural, one {# email} other {# emails}} is unconfirmed. Check your email configuration and resend.',
        { count: unsent },
      ),
    );
  } else {
    toast.success(
      msg(
        'invitations.delivery.sent',
        'Invitation emails submitted for sending. Remind recipients to check their inbox.',
      ),
    );
  }
}
