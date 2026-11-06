import { afterEach, expect, spyOn, test } from 'bun:test';
import '../../test/setup';
import { useWorkspaceManagementHistory } from './use-workspace-management-history';

const { cleanup, renderHook } = await import('@testing-library/react');

afterEach(() => {
  cleanup();
  for (const key of Object.keys(window.localStorage)) {
    if (key.startsWith('oma.workspace-management.last-page:')) window.localStorage.removeItem(key);
  }
});

test('unavailable preference storage does not block navigation or rendering', () => {
  const read = spyOn(window.localStorage, 'getItem').mockImplementation(() => {
    throw new Error('Unavailable');
  });
  const write = spyOn(window.localStorage, 'setItem').mockImplementation(() => {
    throw new Error('Unavailable');
  });
  try {
    const { result } = renderHook(() => useWorkspaceManagementHistory('account', 'org', 'workspace', '/cost'));
    expect(result.current).toBe('/llm-models');
  } finally {
    read.mockRestore();
    write.mockRestore();
  }
});

test('workspace management history is isolated by account, organization and workspace', () => {
  const initial = renderHook(() => useWorkspaceManagementHistory('account', 'org', 'workspace', '/telemetry'));
  initial.unmount();
  const sameScope = renderHook(() => useWorkspaceManagementHistory('account', 'org', 'workspace', undefined));
  expect(sameScope.result.current).toBe('/telemetry');
  sameScope.unmount();
  for (const scope of [
    { account: 'other', org: 'org', workspace: 'workspace' },
    { account: 'account', org: 'other', workspace: 'workspace' },
    { account: 'account', org: 'org', workspace: 'other' },
  ]) {
    const isolated = renderHook(() =>
      useWorkspaceManagementHistory(scope.account, scope.org, scope.workspace, undefined),
    );
    expect(isolated.result.current).toBe('/llm-models');
    isolated.unmount();
  }
});
