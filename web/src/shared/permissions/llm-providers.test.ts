import { expect, test } from 'bun:test';
import { canManageLLMProviders } from './llm-providers';
import type { AuthAccount } from '../auth/api';

const account: AuthAccount = {
  uuid: 'account',
  email_address: 'test@example.com',
  effectiveOrganizationUuid: 'target',
  permissions: ['organization:manage'],
  memberships: [],
};

test('普通账号和伪造成员角色不能授予模型管理', () => {
  expect(
    canManageLLMProviders(
      { ...account, permissions: [], memberships: [{ role: 'admin', organization: { uuid: 'target' } }] },
      'target',
    ),
  ).toBe(false);
  expect(canManageLLMProviders(null, 'target')).toBe(false);
  expect(canManageLLMProviders(account, undefined)).toBe(false);
});

test('当前组织有效权限不能用于第三组织', () => {
  expect(canManageLLMProviders(account, 'other')).toBe(false);
});

test('非成员超管使用服务器有效权限管理模型', () => {
  expect(canManageLLMProviders(account, 'target')).toBe(true);
});
