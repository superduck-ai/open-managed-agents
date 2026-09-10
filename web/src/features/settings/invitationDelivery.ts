import { toast } from '../../shared/ui/sonner';
import type { OrganizationInvite } from './membersApi';

export function notifyInvitationDelivery(invites: OrganizationInvite[]) {
  const unsent = invites.filter((invite) => invite.email_delivery !== 'sent').length;
  if (unsent) {
    toast.warning(`邀请已保存，但 ${unsent} 封邮件未确认发送。请检查邮件配置后重发邀请。`);
  } else {
    toast.success('邀请邮件已提交发送，请提醒收件人检查邮箱。');
  }
}
