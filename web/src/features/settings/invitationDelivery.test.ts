import { afterAll, afterEach, expect, spyOn, test } from 'bun:test';
import { toast } from '../../shared/ui/sonner';
import { notifyInvitationDelivery } from './invitationDelivery';
import type { OrganizationInvite } from './membersApi';

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

test('未配置或发送失败不得显示发送成功', () => {
  notifyInvitationDelivery([
    { email_delivery: 'failed' },
    { email_delivery: 'not_configured' },
  ] as OrganizationInvite[]);
  expect(warning).toHaveBeenCalledWith('邀请已保存，但 2 封邮件未确认发送。请检查邮件配置后重发邀请。');
  expect(success).not.toHaveBeenCalled();
});

test('服务端确认 SMTP 接受才显示提交发送', () => {
  notifyInvitationDelivery([{ email_delivery: 'sent' }] as OrganizationInvite[]);
  expect(success).toHaveBeenCalledWith('邀请邮件已提交发送，请提醒收件人检查邮箱。');
  expect(warning).not.toHaveBeenCalled();
});
