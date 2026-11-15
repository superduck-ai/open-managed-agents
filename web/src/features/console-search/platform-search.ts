import { Blocks, Building2, ShieldCheck } from 'lucide-react';
import type { PlatformAdminClient } from '../../shared/api/platform-admin';
import type { I18nContextValue } from '../../shared/i18n/context';
import type { Organization, Page } from '../platform-admin/types';
import type { ResourceCursor } from './resource-search';
import type { SearchRecord } from './search-data';

export function platformNavigationSearchRecords(permissions: string[], msg: I18nContextValue['msg']): SearchRecord[] {
  const pages = [
    {
      href: '/platform-admin',
      labelId: 'platformAdmin.organizations',
      label: 'Organizations',
      icon: Building2,
      permission: 'platform:organizations:read',
    },
    {
      href: '/platform-admin/administrators',
      labelId: 'platformAdmin.superAdministrators',
      label: 'Super administrators',
      icon: ShieldCheck,
      permission: 'platform:administrators:manage',
    },
    {
      href: '/platform-admin/skills',
      labelId: 'systemSkills.title',
      label: 'System skills',
      icon: Blocks,
      permission: 'platform:skills:manage',
    },
  ];
  return pages
    .filter((page) => permissions.includes(page.permission))
    .map((page) => ({
      id: page.href,
      href: page.href,
      name: msg(page.labelId, page.label),
      context: msg('platformAdmin.platformAdministration', 'Super admin console'),
      kind: 'page',
      icon: page.icon,
    }));
}

export async function searchOrganizationPage(
  client: PlatformAdminClient,
  query: string,
  cursor: ResourceCursor,
  signal: AbortSignal,
) {
  const page = await client.request<Page<Organization>>(
    `/organizations?${new URLSearchParams({ query, cursor: cursor.organizations || '' })}`,
    { signal },
  );
  const records: SearchRecord[] = page.data.map((organization) => ({
    id: organization.uuid,
    name: organization.name,
    context: organization.uuid,
    href: `/platform-admin/organizations/${encodeURIComponent(organization.uuid)}`,
    kind: 'resource',
    icon: Building2,
  }));
  return { records, errors: [] as string[], next: page.next_cursor ? { organizations: page.next_cursor } : undefined };
}
