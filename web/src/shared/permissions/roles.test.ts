import { describe, expect, test } from 'bun:test';
import { accountRoleLabel, roleOptions } from './roles';

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
});
