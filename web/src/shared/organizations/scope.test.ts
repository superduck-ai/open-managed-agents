import { describe, expect, test } from 'bun:test';
import { chooseWorkspace, fallbackOrganization, isBusinessQuery, scopedAccount, scopePermissions } from './scope';
import { canManageMembers } from '../permissions/members';
import type { AuthAccount } from '../auth/api';

const account: AuthAccount = {
  uuid: 'account',
  email_address: 'a@example.com',
  default_organization_uuid: 'b',
  permissions: ['members:manage'],
  memberships: [
    { organization: { uuid: 'a' }, role: 'admin', user_id: 'user-a', user_uuid: 'uuid-a' },
    { organization: { uuid: 'b' }, role: 'user', user_id: 'user-b', user_uuid: 'uuid-b' },
  ],
};

describe('组织作用域策略', () => {
  test('无成员组织时不会复用旧组织或伪造默认组织', () => {
    expect(fallbackOrganization({ ...account, memberships: [] }, 'a')).toBeUndefined();
    expect(chooseWorkspace([], 'default')).toBeUndefined();
    expect(chooseWorkspace([{ id: 'custom', name: '自定义', type: 'workspace' }], 'missing')).toBeUndefined();
  });
  test('移除当前组织时优先账号有效默认组织', () => {
    expect(fallbackOrganization(account, 'removed')).toBe('b');
    expect(fallbackOrganization({ ...account, default_organization_uuid: 'removed' }, 'removed')).toBe('a');
  });
  test('保留真实默认工作区身份且优先最近有效工作区', () => {
    const workspaces = [
      { id: 'real-default', name: 'Default', type: 'workspace' as const, is_default: true },
      { id: 'recent', name: 'Recent', type: 'workspace' as const },
    ];
    expect(chooseWorkspace(workspaces, 'removed')?.id).toBe('real-default');
    expect(chooseWorkspace(workspaces, 'recent')?.id).toBe('recent');
  });
  test('组织管理员身份不能泄漏到另一个组织', () => {
    const projected = scopedAccount(account, 'b', {
      id: 'ws',
      type: 'workspace',
      name: 'Workspace',
      effective_role: 'workspace_user',
    });
    expect(canManageMembers(projected)).toBe(false);
    expect(projected?.memberships?.[0].user_id).toBe('user-b');
    expect(projected?.uuid).toBe('account');
    expect(scopePermissions('user', 'workspace_admin')).not.toContain('members:manage');
    expect(scopePermissions('admin', 'workspace_admin')).toContain('members:manage');
  });
  test('未授权工作区不能因组织 Billing 身份获得权限', () => {
    expect(scopePermissions('billing')).toEqual([]);
    expect(scopePermissions('user', 'workspace_user')).not.toContain('api:manage');
    expect(scopePermissions('user', 'workspace_billing')).not.toContain('api:manage');
  });
  test('Billing 切换仅投影当前组织权限并保持 #346 基线', () => {
    const projected = scopedAccount(
      {
        ...account,
        memberships: [...account.memberships!, { organization: { uuid: 'c' }, role: 'billing', user_id: 'user-c' }],
      },
      'c',
      { id: 'ws-c', type: 'workspace', name: 'Workspace', effective_role: 'workspace_billing' },
    );
    expect(projected?.memberships?.[0]?.user_id).toBe('user-c');
    expect(projected?.permissions).toEqual(scopePermissions('billing', 'workspace_billing'));
    expect(projected?.permissions).not.toContain('members:manage');
    expect(projected?.permissions).not.toContain('workspace:members:manage');
    expect(projected?.permissions).toContain('workbench:view');
    expect(projected?.permissions).toContain('api:manage');
    expect(projected?.permissions).toContain('billing:view');
  });
  test('工作区角色缺失时回退当前组织角色判权', () => {
    // 工作区列表未加载或 activeWorkspace 为占位对象时，scope 推导不出权限；
    // 此时不能写入空 permissions 短路判权，应回退到当前组织的 membership 角色。
    const projected = scopedAccount(account, 'a', { id: '', type: 'workspace', name: '' });
    expect(projected?.permissions).toBeUndefined();
    expect(canManageMembers(projected, 'a')).toBe(true);
    expect(canManageMembers(scopedAccount(account, 'b', { id: '', type: 'workspace', name: '' }), 'b')).toBe(false);
  });
  test('所有业务前缀均被取消且保留账号级查询', () => {
    for (const prefix of [
      'console',
      'files',
      'skills',
      'skill',
      'skillVersions',
      'agent-config',
      'managed-agents',
      'observability',
      'messageBatches',
      'workspace-members',
      'new-feature',
    ])
      expect(isBusinessQuery({ queryKey: [prefix] })).toBe(true);
    expect(isBusinessQuery({ queryKey: ['auth', 'bootstrap'] })).toBe(false);
    expect(isBusinessQuery({ queryKey: ['invitations'] })).toBe(false);
  });
});
