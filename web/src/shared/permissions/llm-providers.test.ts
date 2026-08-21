import { describe, expect, test } from 'bun:test';
import type { AuthAccount } from '../auth/api';
import { canManageLLMProviders } from './llm-providers';

const account: AuthAccount = {
  uuid: 'acct_test',
  email_address: 'test@example.com',
  memberships: [
    { role: 'developer', organization: { uuid: 'org_developer' } },
    { role: 'ADMIN', organization: { uuid: 'org_admin' } },
  ],
};

describe('canManageLLMProviders', () => {
  test('allows an administrator in the current organization', () => {
    expect(canManageLLMProviders(account, 'org_admin')).toBe(true);
  });

  test('does not reuse administrator access from another organization', () => {
    expect(canManageLLMProviders(account, 'org_developer')).toBe(false);
  });

  test('denies access without an authenticated account or organization', () => {
    expect(canManageLLMProviders(null, 'org_admin')).toBe(false);
    expect(canManageLLMProviders(account, undefined)).toBe(false);
  });
});
