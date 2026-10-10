import { describe, expect, test } from 'bun:test';
import { accountRoleLabel, membershipRoleForOrganization, roleOptions } from './roles';

const msg = (_id: string, defaultMessage: string) => defaultMessage;

describe('roleOptions', () => {
  test('matches the supported platform roles', () => {
    expect(roleOptions.map((role) => role.value)).toEqual(['user', 'admin']);
  });

  test('does not treat inherited object keys as platform roles', () => {
    expect(accountRoleLabel('constructor', msg)).toBe('User');
    expect(accountRoleLabel('toString', msg)).toBe('User');
    expect(accountRoleLabel('user', msg)).toBe('User');
    expect(accountRoleLabel('admin', msg)).toBe('Admin');
    expect(accountRoleLabel('workspace_admin', msg)).toBe('Admin');
    expect(accountRoleLabel('workspace_user', msg)).toBe('User');
    expect(accountRoleLabel(undefined, msg)).toBe('User');
    expect(accountRoleLabel('', msg)).toBe('User');
  });

  test('uses the membership for the active organization', () => {
    const memberships = [
      { role: 'user', organization: { uuid: 'org_other' } },
      { role: 'admin', organization: { uuid: 'org_active' } },
    ];

    expect(membershipRoleForOrganization(memberships, 'org_active')).toBe('admin');
    expect(accountRoleLabel(membershipRoleForOrganization(memberships, 'org_active'), msg)).toBe('Admin');
    expect(membershipRoleForOrganization(memberships, undefined)).toBe('user');
    expect(membershipRoleForOrganization(memberships, 'org_missing')).toBeUndefined();
    expect(membershipRoleForOrganization(undefined, 'org_active')).toBeUndefined();
  });
});
