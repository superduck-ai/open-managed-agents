import type { QueryClient } from '@tanstack/react-query';
import { listAgentPickerPage } from '../managed-agents/agents/agent-picker-api';
import { listManagedEntities } from '../managed-agents/api';
import { listWorkspaceMCPServers } from '../../shared/api/workspaceMCPServers';
import { anthropicBetaApi } from '../../shared/api/anthropic';
import type { ManagedEntitySection } from '../managed-agents/types';
import type { I18nContextValue } from '../../shared/i18n/context';
import { consoleNavigation } from '../../shared/navigation/navigation';
import { navigationHref } from '../../shared/navigation/navigation-paths';
import type { SearchRecord } from './search-data';

const managedSources: ManagedEntitySection[] = [
  'sessions',
  'deployments',
  'environments',
  'memory-stores',
  'credential-vaults',
];
type ResourceSource = 'agents' | ManagedEntitySection | 'mcp-servers' | 'files' | 'skills';
const sources: ResourceSource[] = ['agents', ...managedSources, 'mcp-servers', 'files', 'skills'];
const resourceNavigation = consoleNavigation.flatMap((item) => (item.type === 'group' ? item.children : [item]));

export function resourceContextLabel(source: string, msg: I18nContextValue['msg']) {
  const item = resourceNavigation.find((entry) => entry.href === `/${source}`);
  return item ? msg(item.labelId, item.label) : source;
}
type ResourceSummary = {
  id: string;
  name?: string;
  title?: string | null;
  display_name?: string;
  filename?: string;
  display_title?: string;
};
type ResourcePage = { data: ResourceSummary[]; next_page?: string | null; has_more?: boolean; last_id?: string | null };
export type ResourceCursor = Partial<Record<ResourceSource | 'organizations', string | null>>;

async function readPage(
  source: ResourceSource,
  organizationId: string,
  workspaceId: string,
  query: string,
  page?: string,
): Promise<ResourcePage> {
  if (source === 'agents') return listAgentPickerPage(workspaceId, query, page);
  if (source === 'mcp-servers') return listWorkspaceMCPServers(organizationId, workspaceId, { search: query, page });
  if (source === 'files')
    return anthropicBetaApi.files.list<ResourceSummary>(
      { limit: 50, ...(page ? { after_id: page } : {}) },
      workspaceId,
    );
  if (source === 'skills')
    return anthropicBetaApi.skills.list<ResourceSummary>({ limit: 50, ...(page ? { page } : {}) }, workspaceId);
  return listManagedEntities(source, workspaceId, page);
}

export async function searchResourcePage(
  queryClient: QueryClient,
  organizationId: string,
  workspaceId: string,
  query: string,
  cursor: ResourceCursor,
) {
  const active = sources.filter((source) => cursor[source] !== null);
  const settled = await Promise.allSettled(
    active.map(async (source) => {
      const pageCursor = cursor[source] || undefined;
      const serverFiltered = source === 'agents' || source === 'mcp-servers';
      const page = serverFiltered
        ? await readPage(source, organizationId, workspaceId, query, pageCursor)
        : await queryClient.fetchQuery({
            queryKey: ['console-search-resource', organizationId, workspaceId, source, pageCursor || ''],
            queryFn: () => readPage(source, organizationId, workspaceId, '', pageCursor),
            staleTime: 30000,
          });
      return { source, page };
    }),
  );
  const next: ResourceCursor = { ...cursor };
  const records: SearchRecord[] = [];
  const errors: string[] = [];
  settled.forEach((result, index) => {
    const source = active[index];
    if (result.status === 'rejected') {
      errors.push(source);
      next[source] = null;
      return;
    }
    const page = result.value.page;
    next[source] = page.next_page || (page.has_more ? page.last_id || null : null);
    records.push(
      ...page.data.map((item): SearchRecord => ({
        id: item.id,
        name: item.name || item.title || item.display_name || item.filename || item.display_title || item.id,
        context: source,
        kind: 'resource',
        icon: resourceNavigation.find((item) => item.href === `/${source}`)?.icon,
        href: `${navigationHref(`/${source}`, workspaceId)}/${encodeURIComponent(item.id)}`,
      })),
    );
  });
  return { records, errors, next: Object.values(next).some(Boolean) ? next : undefined };
}
