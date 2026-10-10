import { expect, test } from 'bun:test';
import { invitationErrorMessage, organizationRoleLabel } from './api';

test('网络及未知状态使用可重试文案，未知冲突不会误报已过期', () => {
  expect(invitationErrorMessage(new TypeError('offline'))).toBe('操作失败，请重试。');
  expect(invitationErrorMessage(null, '加载邀请失败。')).toBe('加载邀请失败。');
  expect(invitationErrorMessage({ status: 409, message: 'other conflict' })).toBe('此邀请状态已变更，请刷新后重试。');
});

test('组织只有用户和管理员角色', () => {
  expect(['admin', 'user'].map(organizationRoleLabel)).toEqual(['管理员', '用户']);
  expect(organizationRoleLabel()).toBe('成员');
});
