import { expect, test } from 'bun:test';
import { canManageMembers } from './members';

test('unsupported and user roles cannot manage organization members', () => {
  expect(canManageMembers(undefined)).toBe(false);
  for (const role of [
    'user',
    'workspace_admin',
    'owner',
    'primary_owner',
    'membership_admin',
    'billing',
    'developer',
  ]) {
    expect(canManageMembers({ uuid: 'account', memberships: [{ role }] })).toBe(false);
  }
});

test('explicit permissions take precedence over role labels', () => {
  expect(canManageMembers({ uuid: 'account', memberships: [{ role: 'admin' }], permissions: [] })).toBe(false);
  expect(canManageMembers({ uuid: 'account', memberships: [{ role: 'user' }], permissions: ['members:manage'] })).toBe(
    true,
  );
  expect(canManageMembers({ uuid: 'account', memberships: [{ role: 'admin' }] })).toBe(true);
});

test('其他组织的管理员身份不能管理当前组织成员', () => {
  const account = {
    uuid: 'account',
    email_address: 'member@example.com',
    memberships: [
      { role: 'admin', organization: { uuid: 'org_admin' } },
      { role: 'user', organization: { uuid: 'org_member' } },
    ],
  };
  expect(canManageMembers(account, 'org_member')).toBe(false);
  expect(canManageMembers(account, 'unknown')).toBe(false);
  expect(canManageMembers(account, 'org_admin')).toBe(true);
});
