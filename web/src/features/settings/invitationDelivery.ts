import { toast } from '../../shared/ui/sonner';
import type { OrganizationInvite } from './membersApi';
import type { I18nContextValue } from '../../shared/i18n/context';

export function notifyInvitationDelivery(invites: OrganizationInvite[], msg: I18nContextValue['msg']) {
  if (!invites.length) return;

  const sent = invites.filter((invite) => invite.email_delivery === 'sent').length;
  const failed = invites.filter((invite) => invite.email_delivery === 'failed').length;
  const notConfigured = invites.filter((invite) => invite.email_delivery === 'not_configured').length;
  const unknown = invites.length - sent - failed - notConfigured;
  const description = [
    sent &&
      msg(
        'invitations.delivery.sent',
        'Invitation emails submitted for sending. Remind recipients to check their inbox.',
      ),
    failed &&
      msg(
        'invitations.delivery.failed',
        '{count, plural, one {# invitation email} other {# invitation emails}} could not be sent. You can resend from the invitation menu.',
        { count: failed },
      ),
    notConfigured &&
      msg(
        'invitations.delivery.notConfigured',
        'Email delivery is not configured. Recipients can sign in with the invited email address to view and accept their invitations.',
      ),
    unknown &&
      msg(
        'invitations.delivery.unconfirmed',
        'The server did not return the delivery status of {count, plural, one {# invitation email} other {# invitation emails}}. Recipients can sign in to view their invitations.',
        { count: unknown },
      ),
  ]
    .filter(Boolean)
    .join(' ');
  const notify = failed || unknown ? toast.warning : toast.success;
  notify(msg('invitations.delivery.created', 'Invitation created successfully.'), { description });
}
