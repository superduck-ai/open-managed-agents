import { expect, test } from 'bun:test';
import { canManageMembers } from './members';

test('其他组织的管理员身份不能管理当前组织成员', () => {
  const account = {
    uuid: 'account',
    email_address: 'member@example.com',
    memberships: [
      { role: 'admin', organization: { uuid: 'org_admin' } },
      { role: 'developer', organization: { uuid: 'org_member' } },
    ],
  };
  expect(canManageMembers(account, 'org_member')).toBe(false);
  expect(canManageMembers(account, 'unknown')).toBe(false);
  expect(canManageMembers(account, 'org_admin')).toBe(true);
});
