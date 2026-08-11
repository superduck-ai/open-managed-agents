import { describe, expect, test } from 'bun:test';
import { canRefreshModelCatalog } from './model-catalog';

describe('model catalog permissions', () => {
  test('allows only an admin membership in the active organization', () => {
    const account = (role: string, organizationUUID: string) => ({
      uuid: 'account-1',
      email_address: 'admin@example.test',
      memberships: [{ role, organization: { uuid: organizationUUID } }],
    });
    expect(canRefreshModelCatalog(account('admin', 'org-1'), 'org-1')).toBe(true);
    expect(canRefreshModelCatalog(account('developer', 'org-1'), 'org-1')).toBe(false);
    expect(canRefreshModelCatalog(account('admin', 'org-2'), 'org-1')).toBe(false);
  });
});
