import { expect, test } from 'bun:test';
import { canViewAnalytics } from './analytics';

const account = {
  uuid: 'a',
  email_address: 'a@example.com',
  effectiveOrganizationUuid: 'org',
  permissions: ['usage:view'],
};

test('analytics access requires the matching organization and report permission', () => {
  expect(canViewAnalytics(null, 'org', 'usage:view')).toBe(false);
  expect(canViewAnalytics(account, 'other', 'usage:view')).toBe(false);
  expect(canViewAnalytics(account, 'org', 'cost:view')).toBe(false);
  expect(canViewAnalytics({ ...account, permissions: [] }, 'org', 'usage:view')).toBe(false);
});

test.each(['usage:view', 'cost:view'] as const)(
  'analytics access accepts %s within the current organization',
  (permission) => {
    expect(canViewAnalytics({ ...account, permissions: [permission] }, 'org', permission)).toBe(true);
  },
);
