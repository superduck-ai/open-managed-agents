import { afterEach, expect, test } from 'bun:test';
import {
  readPlatformScope,
  savePlatformScope,
  clearPlatformScope,
  isPlatformOrganizationAccess,
} from './platform-scope';
import type { OrganizationAccess } from '../auth/api';
afterEach(() => window.sessionStorage.clear());
test('错误标签数据及其他账号不能恢复进入目标', () => {
  window.sessionStorage.setItem('oma.platform-organization', '{');
  expect(readPlatformScope('account')).toBeUndefined();
  savePlatformScope({ accountUuid: 'other', targetOrganizationUuid: 'target' });
  expect(readPlatformScope('account')).toBeUndefined();
});
test('标签保存目标上下文且退出只清进入偏好', () => {
  const scope = {
    accountUuid: 'account',
    targetOrganizationUuid: 'target',
    targetWorkspaceId: 'workspace',
  };
  savePlatformScope(scope);
  expect(readPlatformScope('account')).toEqual(scope);
  window.sessionStorage.setItem('oma.organization.account', 'home');
  clearPlatformScope();
  expect(readPlatformScope('account')).toBeUndefined();
  expect(window.sessionStorage.getItem('oma.organization.account')).toBe('home');
});

test('存储不可用时保存与清理静默降级', () => {
  const original = window.sessionStorage;
  Object.defineProperty(window, 'sessionStorage', {
    configurable: true,
    get() {
      throw new Error('storage blocked');
    },
  });
  try {
    expect(() => savePlatformScope({ accountUuid: 'account', targetOrganizationUuid: 'target' })).not.toThrow();
    expect(() => clearPlatformScope()).not.toThrow();
  } finally {
    Object.defineProperty(window, 'sessionStorage', { configurable: true, value: original });
  }
});

test('平台组织访问只接受目标一致的平台来源', () => {
  const access: OrganizationAccess = {
    source: 'platform_admin',
    organization: { uuid: 'target', name: 'Target' },
    organization_role: 'admin',
    workspace_role: 'workspace_admin',
  };
  expect(isPlatformOrganizationAccess(undefined, 'target')).toBe(false);
  expect(isPlatformOrganizationAccess(null, 'target')).toBe(false);
  expect(isPlatformOrganizationAccess({ ...access, source: 'organization_member' }, 'target')).toBe(false);
  expect(isPlatformOrganizationAccess(access, 'other')).toBe(false);
  expect(isPlatformOrganizationAccess(access, 'target')).toBe(true);
});
