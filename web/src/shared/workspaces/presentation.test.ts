import { expect, test } from 'bun:test';
import { workspaceSwitchPath } from './presentation';

test.each(['agents', 'sessions', 'deployments', 'environments', 'vaults', 'memory-stores', 'skills'])(
  '切换 %s 详情时不保留旧资源与筛选条件',
  (section) => {
    expect(workspaceSwitchPath(`/workspaces/default/${section}/old-resource/details?agent=old#tab`, 'other')).toBe(
      `/workspaces/other/${section}`,
    );
  },
);

test.each(['files', 'batches', 'llm-models', 'playground', 'observability', 'dreams', 'cost', 'logs'])(
  '切换 %s 列表进入目标工作区',
  (section) => {
    expect(workspaceSwitchPath(`/workspaces/default/${section}`, 'other')).toBe(`/workspaces/other/${section}`);
    expect(workspaceSwitchPath(`/${section}`, 'other')).toBe(`/workspaces/other/${section}`);
  },
);

test('组织设置保持原路由，工作区设置使用新作用域', () => {
  expect(workspaceSwitchPath('/settings/members', 'other')).toBe('/settings/members');
  expect(workspaceSwitchPath('/members', 'other')).toBe('/members');
  expect(workspaceSwitchPath('/settings/workspaces/default/keys', 'other')).toBe('/settings/workspaces/other/keys');
  expect(workspaceSwitchPath('/settings/workspaces/default/webhooks', 'other')).toBe(
    '/settings/workspaces/other/webhooks',
  );
});

test('旧入口与未指定工作区仍使用兼容路由', () => {
  expect(workspaceSwitchPath('/agents/old', '')).toBe('/workspaces/default/agents');
  expect(workspaceSwitchPath('/skills/old', 'other')).toBe('/workspaces/other/skills');
  expect(workspaceSwitchPath('/credential-vaults', 'other')).toBe('/workspaces/other/vaults');
  expect(workspaceSwitchPath('/quickstart', 'other')).toBe('/workspaces/other/agent-quickstart');
  expect(workspaceSwitchPath('/workbench/old', 'other')).toBe('/workbench');
});
