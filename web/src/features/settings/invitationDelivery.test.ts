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
  const messages = locale === 'en' ? en : zhCn;
  const intl = createIntl({ locale, messages });
  const msg: I18nContextValue['msg'] = (id, defaultMessage, values) =>
    intl.formatMessage({ id, defaultMessage }, values);
  const title = messages['invitations.delivery.created'];
  const notify = (...statuses: OrganizationInvite['email_delivery'][]) =>
    notifyInvitationDelivery(
      statuses.map((emailDelivery) => ({ email_delivery: emailDelivery })) as OrganizationInvite[],
      msg,
    );

  for (const count of [1, 2]) {
    test(`${locale} 邮件失败仍确认邀请成功，准确显示失败数量 ${count}`, () => {
      notify(...Array<OrganizationInvite['email_delivery']>(count).fill('failed'));
      expect(warning).toHaveBeenCalledWith(title, {
        description:
          locale === 'en'
            ? `${count} invitation ${count === 1 ? 'email' : 'emails'} could not be sent. You can resend from the invitation menu.`
            : `${count} 封邀请邮件发送失败，可在邀请菜单中重发。`,
      });
      expect(success).not.toHaveBeenCalled();
    });
  }

  test(`${locale} 缺少投递状态不归因为配置错误`, () => {
    notify(undefined);
    expect(warning).toHaveBeenCalledWith(title, {
      description:
        locale === 'en'
          ? 'The server did not return the delivery status of 1 invitation email. Recipients can sign in to view their invitations.'
          : '服务端未返回 1 封邀请邮件的投递状态，受邀人可登录查看组织邀请。',
    });
    expect(success).not.toHaveBeenCalled();
  });

  test(`${locale} 混合批次仅统计实际失败邮件，分别说明其他状态`, () => {
    notify('sent', 'failed', 'not_configured', undefined);
    expect(warning).toHaveBeenCalledTimes(1);
    expect(warning).toHaveBeenCalledWith(title, {
      description: [
        msg('invitations.delivery.sent', ''),
        msg('invitations.delivery.failed', '', { count: 1 }),
        msg('invitations.delivery.notConfigured', ''),
        msg('invitations.delivery.unconfirmed', '', { count: 1 }),
      ].join(' '),
    });
    expect(success).not.toHaveBeenCalled();
  });

  test(`${locale} 未配置邮件仍提示邀请成功并说明站内接受方式`, () => {
    notify('not_configured', 'not_configured');
    expect(success).toHaveBeenCalledWith(title, {
      description: messages['invitations.delivery.notConfigured'],
    });
    expect(warning).not.toHaveBeenCalled();
  });

  test(`${locale} 服务端确认 SMTP 接受才显示提交发送`, () => {
    notify('sent');
    expect(success).toHaveBeenCalledWith(title, { description: messages['invitations.delivery.sent'] });
    expect(warning).not.toHaveBeenCalled();
  });

  test(`${locale} 空批次不显示成功或警告`, () => {
    notify();
    expect(warning).not.toHaveBeenCalled();
    expect(success).not.toHaveBeenCalled();
  });
}
