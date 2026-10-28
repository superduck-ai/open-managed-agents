import { afterEach, expect, test } from 'bun:test';
import { readPlatformScope, savePlatformScope, clearPlatformScope } from './platform-scope';
afterEach(() => window.sessionStorage.clear());
test('错误标签数据及其他账号不能恢复进入目标', () => {
  window.sessionStorage.setItem('oma.platform-organization', '{');
  expect(readPlatformScope('account')).toBeUndefined();
  savePlatformScope({ accountUuid: 'other', targetOrganizationUuid: 'target' });
  expect(readPlatformScope('account')).toBeUndefined();
});
test('标签保存真实恢复上下文且退出只清进入偏好', () => {
  const scope = {
    accountUuid: 'account',
    targetOrganizationUuid: 'target',
    returnOrganizationUuid: 'home',
    returnWorkspaceId: 'workspace',
  };
  savePlatformScope(scope);
  expect(readPlatformScope('account')).toEqual(scope);
  window.sessionStorage.setItem('oma.organization.account', 'home');
  clearPlatformScope();
  expect(readPlatformScope('account')).toBeUndefined();
  expect(window.sessionStorage.getItem('oma.organization.account')).toBe('home');
});
