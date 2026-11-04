import { expect, test } from 'bun:test';
import { canManageOrganizationLLMProviders, canManageWorkspaceLLMProviders } from './llm-providers';
import type { AuthAccount } from '../auth/api';

const account: AuthAccount = {
  uuid: 'account',
  email_address: 'test@example.com',
  effectiveOrganizationUuid: 'target',
  permissions: ['organization:manage'],
  memberships: [],
};

test('普通账号和伪造成员角色不能授予组织模型管理', () => {
  expect(
    canManageOrganizationLLMProviders(
      { ...account, permissions: [], memberships: [{ role: 'admin', organization: { uuid: 'target' } }] },
      'target',
    ),
  ).toBe(false);
  expect(canManageOrganizationLLMProviders(null, 'target')).toBe(false);
  expect(canManageOrganizationLLMProviders(account, undefined)).toBe(false);
});

test('当前组织有效权限不能用于第三组织', () => {
  expect(canManageOrganizationLLMProviders(account, 'other')).toBe(false);
});

test('非成员超管使用服务器有效权限管理模型', () => {
  expect(canManageOrganizationLLMProviders(account, 'target')).toBe(true);
});

test('工作区成员使用工作区资源权限管理模型', () => {
  expect(canManageWorkspaceLLMProviders({ ...account, permissions: ['workspace:api:resource_manage'] })).toBe(true);
  expect(canManageWorkspaceLLMProviders({ ...account, permissions: ['organization:manage'] })).toBe(false);
  expect(canManageWorkspaceLLMProviders({ ...account, permissions: [] })).toBe(false);
  expect(canManageWorkspaceLLMProviders(null)).toBe(false);
});
