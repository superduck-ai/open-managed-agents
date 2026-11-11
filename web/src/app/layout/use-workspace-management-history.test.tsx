import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { readWorkspaceManagementOrigin, saveWorkspaceManagementOrigin } from './use-workspace-management-history';

const { cleanup } = await import('@testing-library/react');

afterEach(() => {
  cleanup();
  window.sessionStorage.clear();
  for (const key of Object.keys(window.localStorage)) {
    if (key.startsWith('oma.workspace-management.last-page:')) window.localStorage.removeItem(key);
  }
});

test('management origin survives remounts without crossing account or organization boundaries', () => {
  const origin = { href: '/workspaces/a/agent-quickstart?mode=api#online-test', workspaceId: 'a' };
  saveWorkspaceManagementOrigin('account', 'org', origin);
  expect(readWorkspaceManagementOrigin('account', 'org')).toEqual(origin);
  expect(readWorkspaceManagementOrigin('other', 'org')).toBeNull();
  expect(readWorkspaceManagementOrigin('account', 'other')).toBeNull();
  saveWorkspaceManagementOrigin('account', 'org', { ...origin, href: '//external.example/' });
  expect(readWorkspaceManagementOrigin('account', 'org')).toBeNull();
});

test('unavailable session storage does not break management navigation', () => {
  const storage = window.sessionStorage;
  Object.defineProperty(window, 'sessionStorage', {
    configurable: true,
    get() {
      throw new Error('Unavailable');
    },
  });
  try {
    expect(() =>
      saveWorkspaceManagementOrigin('account', 'org', { href: '/dashboard', workspaceId: 'a' }),
    ).not.toThrow();
    expect(readWorkspaceManagementOrigin('account', 'org')).toBeNull();
  } finally {
    Object.defineProperty(window, 'sessionStorage', { configurable: true, value: storage });
  }
});
