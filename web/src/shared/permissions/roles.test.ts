import { describe, expect, test } from 'bun:test';
import { roleOptions } from './roles';

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
});
