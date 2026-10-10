import { describe, expect, test } from 'bun:test';
import { accountRoleLabel, membershipRoleForOrganization, roleOptions } from './roles';

const msg = (_id: string, defaultMessage: string) => defaultMessage;

describe('roleOptions', () => {
  test('matches the supported platform roles', () => {
    expect(roleOptions.map((role) => role.value)).toEqual([
      'user',
      'claude_code_user',
      'developer',
      'billing',
      'admin',
    ]);
  });

  test('does not treat inherited object keys as platform roles', () => {
    expect(accountRoleLabel('constructor', msg)).toBe('Constructor');
    expect(accountRoleLabel('toString', msg)).toBe('ToString');
    expect(accountRoleLabel('user', msg)).toBe('User');
    expect(accountRoleLabel('admin', msg)).toBe('Admin');
    expect(accountRoleLabel(undefined, msg)).toBe('Admin');
    expect(accountRoleLabel('', msg)).toBe('Admin');
  });

  test('uses the membership for the active organization', () => {
    const memberships = [
      { role: 'user', organization: { uuid: 'org_other' } },
      { role: 'developer', organization: { uuid: 'org_active' } },
    ];

    expect(membershipRoleForOrganization(memberships, 'org_active')).toBe('developer');
    expect(accountRoleLabel(membershipRoleForOrganization(memberships, 'org_active'), msg)).toBe('Developer');
    expect(membershipRoleForOrganization(memberships, undefined)).toBe('user');
    expect(membershipRoleForOrganization(memberships, 'org_missing')).toBeUndefined();
    expect(membershipRoleForOrganization(undefined, 'org_active')).toBeUndefined();
  });
});
