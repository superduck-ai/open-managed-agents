import { afterAll, afterEach, expect, spyOn, test } from 'bun:test';
import { toast } from '../../shared/ui/sonner';
import { notifyInvitationDelivery } from './invitationDelivery';
import type { OrganizationInvite } from './membersApi';
import { createIntl } from 'react-intl';
import type { I18nContextValue } from '../../shared/i18n/context';
import en from '../../shared/i18n/messages/en.json';
import zhCn from '../../shared/i18n/messages/zh-CN.json';

const warning = spyOn(toast, 'warning').mockImplementation(() => 'warning');
const success = spyOn(toast, 'success').mockImplementation(() => 'success');
afterAll(() => {
  warning.mockRestore();
  success.mockRestore();
});
afterEach(() => {
  warning.mockClear();
  success.mockClear();
});

for (const locale of ['en', 'zh-CN'] as const) {
  const intl = createIntl({ locale, messages: locale === 'en' ? en : zhCn });
  const msg: I18nContextValue['msg'] = (id, defaultMessage, values) =>
    intl.formatMessage({ id, defaultMessage }, values);
  for (const count of [1, 2]) {
    test(`${locale} 未配置或失败显示本地化警告 ${count}`, () => {
      const invites = [{ email_delivery: 'failed' }, { email_delivery: 'not_configured' }].slice(0, count);
      notifyInvitationDelivery(invites as OrganizationInvite[], msg);
      expect(warning).toHaveBeenCalledWith(
        locale === 'en'
          ? `Invitations saved, but delivery of ${count} ${count === 1 ? 'email' : 'emails'} is unconfirmed. Check your email configuration and resend.`
          : `邀请已保存，但 ${count} 封邮件未确认发送。请检查邮件配置后重发邀请。`,
      );
      expect(success).not.toHaveBeenCalled();
    });
  }
  test(`${locale} 缺少服务端状态不视为成功`, () => {
    notifyInvitationDelivery([{}] as OrganizationInvite[], msg);
    expect(warning).toHaveBeenCalledTimes(1);
    expect(success).not.toHaveBeenCalled();
  });
  test(`${locale} 服务端确认 SMTP 接受才显示提交发送`, () => {
    notifyInvitationDelivery([{ email_delivery: 'sent' }] as OrganizationInvite[], msg);
    expect(success).toHaveBeenCalledWith(
      locale === 'en'
        ? 'Invitation emails submitted for sending. Remind recipients to check their inbox.'
        : '邀请邮件已提交发送，请提醒收件人检查邮箱。',
    );
    expect(warning).not.toHaveBeenCalled();
  });
}
