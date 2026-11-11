import { listAgentPickerPage } from '../managed-agents/agents/agent-picker-api';
import { listManagedEntities } from '../managed-agents/api';
import { listWorkspaceMCPServers } from '../../shared/api/workspaceMCPServers';
import { anthropicBetaApi } from '../../shared/api/anthropic';
import type { ManagedEntitySection } from '../managed-agents/types';
import { consoleNavigation } from '../../app/layout/navigation';
import { navigationHref } from '../../app/layout/navigation-paths';
import type { SearchRecord } from './search-data';

const managedSources: ManagedEntitySection[] = [
  'sessions',
  'deployments',
  'environments',
  'memory-stores',
  'credential-vaults',
];
const sources = ['agents', ...managedSources, 'mcp-servers', 'files', 'skills'];
const resourceNavigation = consoleNavigation.flatMap((item) => (item.type === 'group' ? item.children : [item]));
type ResourceSummary = {
  id: string;
  name?: string;
  title?: string | null;
  display_name?: string;
  filename?: string;
  display_title?: string;
};
type ResourcePage = { data: ResourceSummary[]; next_page?: string | null; has_more?: boolean; last_id?: string | null };
export type ResourceCursor = Record<string, string | null>;

async function readPage(
  source: string,
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
  return listManagedEntities(source as ManagedEntitySection, workspaceId, page);
}

export async function searchResourcePage(
  organizationId: string,
  workspaceId: string,
  query: string,
  cursor: ResourceCursor,
) {
  const active = sources.filter((source) => cursor[source] !== null);
  const settled = await Promise.allSettled(
    active.map(async (source) => ({
      source,
      page: await readPage(source, organizationId, workspaceId, query, cursor[source] || undefined),
    })),
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
