import { Bot, Copy } from 'lucide-react';
import { type ReactNode } from 'react';
import { Button } from '../../../shared/ui/button';
import { relativeTime } from '../agents/AgentsResourcePage';
import { localTimezone } from '../api';
import { CompactChip, StatusPill } from '../components/common';
import { type CredentialFormValues, type DeploymentApiResponse, type DeploymentRunApiResponse, type EnvironmentApiResponse, type EnvironmentEditValues, type EnvironmentPackageRow, type ManagedEntityApiResponse, type ManagedEntityFormValues, type ManagedEntitySection, type MemoryApiResponse, type MemoryBranchState, type MemoryTreeNode, type PageResponse, type SessionApiResponse, type VaultApiResponse, type VaultCredentialApiResponse } from '../types';
import { compactEntityId, copyText, formatBytes, objectRecord, titleCase } from '../utils';

export function initialSelectedMemoryId() {
  if (typeof window === 'undefined') {
    return null;
  }
  return new URLSearchParams(window.location.search).get('memory');
}

export function updateMemoryQueryParam(memoryId: string | null) {
  if (typeof window === 'undefined') {
    return;
  }
  const url = new URL(window.location.href);
  if (memoryId) {
    url.searchParams.set('memory', memoryId);
  } else {
    url.searchParams.delete('memory');
  }
  window.history.replaceState(window.history.state, '', `${url.pathname}${url.search}${url.hash}`);
}

export function memoryBranchFromPage(page: PageResponse<MemoryApiResponse>): MemoryBranchState {
  return { loading: false, error: null, data: memoryRowsFromPage(page), prefixes: memoryPrefixPathsFromValues(page.prefixes ?? []) };
}

export function memoryRowsFromPage(page: PageResponse<MemoryApiResponse>) {
  const rows = (page.data ?? []).map(normalizeMemoryRow).filter((memory): memory is MemoryApiResponse => Boolean(memory));
  const existingPaths = new Set(rows.map((memory) => (memory.type === 'memory_prefix' ? normalizeMemoryFolderPath(memory.path) : memory.path)));
  const prefixRows = memoryPrefixPathsFromValues(page.prefixes ?? [])
    .filter((prefix) => !existingPaths.has(prefix))
    .map((prefix) => ({
      id: `prefix:${prefix}`,
      content: null,
      content_size_bytes: 0,
      created_at: '',
      memory_store_id: '',
      path: prefix,
      type: 'memory_prefix' as const
    }));
  return sortMemoryRows([...rows, ...prefixRows]);
}

export function normalizeMemoryRow(memory: MemoryApiResponse) {
  if (typeof memory.path !== 'string' || !memory.path.trim()) {
    return null;
  }
  if (memory.type === 'memory_prefix') {
    const path = normalizeMemoryFolderPath(memory.path);
    return {
      id: memory.id || `prefix:${path}`,
      content: null,
      content_size_bytes: 0,
      created_at: memory.created_at || '',
      memory_store_id: memory.memory_store_id || '',
      path,
      type: 'memory_prefix' as const
    };
  }
  return memory;
}

export function sortMemoryRows(rows: MemoryApiResponse[]) {
  return [...rows].sort((left, right) => String(left.path || '').localeCompare(String(right.path || '')));
}

export function memoryPrefixPathsFromValues(values: unknown[]) {
  const paths = new Set<string>();
  for (const value of values) {
    const path = memoryPrefixPathFromValue(value);
    if (path) {
      paths.add(path);
    }
  }
  return [...paths].sort((left, right) => left.localeCompare(right));
}

export function memoryPrefixPathFromValue(value: unknown) {
  if (typeof value === 'string' && value.trim()) {
    return normalizeMemoryFolderPath(value);
  }
  if (value && typeof value === 'object' && typeof (value as { path?: unknown }).path === 'string') {
    const path = (value as { path: string }).path;
    return path.trim() ? normalizeMemoryFolderPath(path) : null;
  }
  return null;
}

export function normalizeMemoryFolderPath(path: string) {
  const trimmed = path.trim();
  const prefixed = trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
  return prefixed.endsWith('/') ? prefixed : `${prefixed}/`;
}

export function memoryFolderLabel(path: string) {
  return normalizeMemoryFolderPath(path).split('/').filter(Boolean).at(-1) ?? path;
}

export function memoryFileLabel(path: string) {
  return path.trim().split('/').filter(Boolean).at(-1) ?? path;
}

export function memoryFolderPathsFromRows(rows: MemoryApiResponse[]) {
  const paths = new Set<string>();
  for (const row of rows) {
    if (row.type === 'memory_prefix') {
      paths.add(normalizeMemoryFolderPath(row.path));
    }
  }
  return [...paths].sort((left, right) => left.localeCompare(right));
}

export function loadedMemoryRowsFromBranches(rootRows: MemoryApiResponse[], branches: Record<string, MemoryBranchState>) {
  const rows = [...rootRows.filter((memory) => memory.type === 'memory')];
  for (const branch of Object.values(branches)) {
    rows.push(...branch.data.filter((memory) => memory.type === 'memory'));
  }
  return sortMemoryRows(rows);
}

export function memoryFolderPathsFromBranches(rootRows: MemoryApiResponse[], branches: Record<string, MemoryBranchState>) {
  const paths = new Set(memoryFolderPathsFromRows(rootRows));
  for (const branch of Object.values(branches)) {
    for (const path of memoryFolderPathsFromRows(branch.data)) {
      paths.add(path);
    }
  }
  return [...paths].sort((left, right) => left.localeCompare(right));
}

export function buildMemoryTreeNodes(rootRows: MemoryApiResponse[], expandedFolders: Set<string>, branches: Record<string, MemoryBranchState>) {
  const nodes: MemoryTreeNode[] = [];
  const appendRows = (rows: MemoryApiResponse[], depth: number) => {
    const seenFolders = new Set<string>();
    for (const row of sortMemoryRows(rows)) {
      if (row.type === 'memory_prefix') {
        const path = normalizeMemoryFolderPath(row.path);
        if (seenFolders.has(path)) {
          continue;
        }
        seenFolders.add(path);
        const branch = branches[path];
        const expanded = expandedFolders.has(path);
        nodes.push({ type: 'folder', path, label: memoryFolderLabel(path), depth, expanded, loading: Boolean(branch?.loading), error: branch?.error ?? null });
        if (expanded && branch?.data.length) {
          appendRows(branch.data, depth + 1);
        }
        continue;
      }
      nodes.push({ type: 'memory', memory: row, label: memoryFileLabel(row.path), depth });
    }
  };
  appendRows(rootRows, 0);
  return nodes;
}

export function upsertMemoryInBranch(branch: MemoryBranchState, updated: MemoryApiResponse, branchPath: string) {
  const branchFolderPath = normalizeMemoryFolderPath(branchPath);
  const parentFolderPath = memoryParentFolderPath(updated.path);
  const existing = branch.data.some((memory) => memory.id === updated.id);
  const belongsInBranch = parentFolderPath === branchFolderPath;
  if (!existing && !belongsInBranch) {
    return branch;
  }
  const remainingRows = branch.data.filter((memory) => memory.id !== updated.id);
  return { ...branch, data: sortMemoryRows(belongsInBranch ? [updated, ...remainingRows] : remainingRows) };
}

export function upsertMemoryInBranches(branches: Record<string, MemoryBranchState>, updated: MemoryApiResponse) {
  const next: Record<string, MemoryBranchState> = {};
  for (const [path, branch] of Object.entries(branches)) {
    next[path] = upsertMemoryInBranch(branch, updated, path);
  }
  return next;
}

export function memoryParentFolderPath(path: string) {
  const segments = path.trim().split('/').filter(Boolean);
  if (segments.length <= 1) {
    return '/';
  }
  return normalizeMemoryFolderPath(`/${segments.slice(0, -1).join('/')}/`);
}

export function removeMemoryFromBranches(branches: Record<string, MemoryBranchState>, memoryId: string) {
  const next: Record<string, MemoryBranchState> = {};
  for (const [path, branch] of Object.entries(branches)) {
    next[path] = { ...branch, data: branch.data.filter((memory) => memory.id !== memoryId) };
  }
  return next;
}

export function memoryPreviewContent(memory: MemoryApiResponse) {
  if (memory.content) {
    return memory.content;
  }
  const bytes = memory.content_size_bytes ?? 0;
  return bytes ? `${formatBytes(bytes)} stored at ${memory.path}` : '';
}

export function memoryFileName(path: string) {
  const trimmed = path.trim().replace(/\/+$/, '');
  const name = trimmed.split('/').filter(Boolean).pop();
  return name || 'memory.txt';
}

export function cellsForEntity(section: ManagedEntitySection, entity: ManagedEntityApiResponse): Record<string, ReactNode> {
  const idCell = (
    <span className="inline-flex min-w-0 max-w-full items-center gap-2">
      <Button type="button" aria-label={`Copy ${entity.id}`} variant="ghost" size="icon-xs" className="text-muted-foreground hover:bg-secondary" onClick={() => void copyText(entity.id)}>
        <Copy className="size-3.5" aria-hidden />
      </Button>
      <span className="truncate font-mono text-[13px] text-foreground">{compactEntityId(entity.id)}</span>
    </span>
  );
  const status = <StatusPill>{entityStatusLabel(entity)}</StatusPill>;

  switch (section) {
    case 'sessions':
      return {
        ID: idCell,
        Name: entityDisplayName(section, entity),
        Status: status,
        Agent: <CompactChip icon={Bot}>{entityAgentLabel(entity)}</CompactChip>,
        Created: relativeTime(entity.created_at)
      };
    case 'deployments':
      return {
        ID: idCell,
        Name: entityDisplayName(section, entity),
        Status: status,
        Agent: <CompactChip icon={Bot}>{entityAgentLabel(entity)}</CompactChip>,
        Trigger: deploymentTrigger(entity as DeploymentApiResponse),
        Created: relativeTime(entity.created_at)
      };
    case 'environments':
      return {
        ID: idCell,
        Name: entityDisplayName(section, entity),
        Status: status,
        Type: 'Cloud',
        'Updated at': relativeTime(entity.updated_at)
      };
    case 'credential-vaults':
      return {
        ID: idCell,
        Name: entityDisplayName(section, entity),
        Status: status,
        Created: relativeTime(entity.created_at)
      };
    case 'memory-stores':
      return {
        ID: idCell,
        Name: entityDisplayName(section, entity),
        Status: status,
        Created: relativeTime(entity.created_at)
      };
  }
}

export function initialFormValues(section: ManagedEntitySection, entity?: ManagedEntityApiResponse): ManagedEntityFormValues {
  return {
    name: entity ? entityDisplayName(section, entity) : '',
    description: entityDescription(entity),
    agentId: entity ? entityAgentId(entity) : '',
    environmentId: entity && 'environment_id' in entity ? entity.environment_id : '',
    initialMessage: entity ? entityInitialMessage(entity) : '',
    triggerType: entity ? entityTriggerType(entity) : '',
    cronExpression: entity ? entityCronExpression(entity) : '0 9 * * 1',
    timezone: entity ? entityTimezone(entity) : localTimezone(),
    vaultIds: entity ? entityVaultIds(entity) : [],
    memoryStoreIds: entity ? entityMemoryStoreIds(entity) : []
  };
}

export function entityDisplayName(section: ManagedEntitySection, entity: ManagedEntityApiResponse) {
  if (section === 'credential-vaults') {
    return (entity as VaultApiResponse).display_name || entity.id;
  }
  if (section === 'sessions') {
    return (entity as SessionApiResponse).title || entity.id;
  }
  return 'name' in entity && entity.name ? entity.name : entity.id;
}

export function entityDescription(entity?: ManagedEntityApiResponse) {
  if (!entity || !('description' in entity)) {
    return '';
  }
  return entity.description || '';
}

export function entityStatusLabel(entity: ManagedEntityApiResponse) {
  if (entity.archived_at) {
    return 'Archived';
  }
  if ('status' in entity && typeof entity.status === 'string') {
    return titleCase(entity.status);
  }
  if ('state' in entity && typeof entity.state === 'string') {
    return titleCase(entity.state);
  }
  return 'Active';
}

export function entityAgentLabel(entity: ManagedEntityApiResponse) {
  if (!('agent' in entity)) {
    return '—';
  }
  return entityAgentId(entity) || '—';
}

export function entityAgentId(entity: ManagedEntityApiResponse) {
  if (!('agent' in entity)) {
    return '';
  }
  const agent = entity.agent;
  if (typeof agent === 'string') {
    return agent;
  }
  if (agent && typeof agent === 'object') {
    const record = agent as Record<string, unknown>;
    if (typeof record.id === 'string') {
      return record.id;
    }
    if (typeof record.name === 'string') {
      return record.name;
    }
  }
  return '';
}

export function entityVaultIds(entity: ManagedEntityApiResponse) {
  if (!('vault_ids' in entity)) {
    return [];
  }
  const raw = entity.vault_ids;
  return Array.isArray(raw) ? raw.filter((value): value is string => typeof value === 'string') : [];
}

export function entityInitialMessage(entity: ManagedEntityApiResponse) {
  if (!('initial_events' in entity) || !Array.isArray(entity.initial_events)) {
    return '';
  }
  for (const event of entity.initial_events) {
    if (!event || typeof event !== 'object') {
      continue;
    }
    const content = (event as { content?: unknown }).content;
    if (!Array.isArray(content)) {
      continue;
    }
    const text = content
      .map((item) => (item && typeof item === 'object' && typeof (item as { text?: unknown }).text === 'string' ? (item as { text: string }).text : ''))
      .join('')
      .trim();
    if (text) {
      return text;
    }
  }
  return '';
}

export function entityMemoryStoreIds(entity: ManagedEntityApiResponse) {
  if (!('resources' in entity) || !Array.isArray(entity.resources)) {
    return [];
  }
  return entity.resources
    .map((resource) =>
      resource && typeof resource === 'object' && typeof (resource as { memory_store_id?: unknown }).memory_store_id === 'string'
        ? (resource as { memory_store_id: string }).memory_store_id
        : null
    )
    .filter((item): item is string => Boolean(item));
}

export function entityTriggerType(entity: ManagedEntityApiResponse): ManagedEntityFormValues['triggerType'] {
  if (!('schedule' in entity)) {
    return '';
  }
  return entity.schedule && typeof entity.schedule === 'object' ? 'schedule' : 'manual';
}

export function entityCronExpression(entity: ManagedEntityApiResponse) {
  if (!('schedule' in entity) || !entity.schedule || typeof entity.schedule !== 'object') {
    return '0 9 * * 1';
  }
  const expression = (entity.schedule as { expression?: unknown }).expression;
  return typeof expression === 'string' && expression.trim() ? expression : '0 9 * * 1';
}

export function entityTimezone(entity: ManagedEntityApiResponse) {
  if (!('schedule' in entity) || !entity.schedule || typeof entity.schedule !== 'object') {
    return localTimezone();
  }
  const timezone = (entity.schedule as { timezone?: unknown }).timezone;
  return typeof timezone === 'string' && timezone.trim() ? timezone : localTimezone();
}

export function detailRowsForEntity(section: ManagedEntitySection, entity: ManagedEntityApiResponse) {
  switch (section) {
    case 'sessions':
      return [
        { label: 'Status', value: entityStatusLabel(entity) },
        { label: 'Agent', value: entityAgentLabel(entity) },
        { label: 'Environment', value: (entity as SessionApiResponse).environment_id || '—' },
        { label: 'Deployment', value: (entity as SessionApiResponse).deployment_id || '—' }
      ];
    case 'deployments':
      return [
        { label: 'Status', value: entityStatusLabel(entity) },
        { label: 'Agent', value: entityAgentLabel(entity) },
        { label: 'Environment', value: (entity as DeploymentApiResponse).environment_id || '—' },
        { label: 'Trigger', value: deploymentTrigger(entity as DeploymentApiResponse) }
      ];
    case 'environments':
      return [
        { label: 'Status', value: entityStatusLabel(entity) },
        { label: 'Type', value: 'Cloud' },
        { label: 'Scope', value: (entity as EnvironmentApiResponse).scope || 'workspace' },
        { label: 'Created', value: relativeTime(entity.created_at) }
      ];
    case 'credential-vaults':
      return [
        { label: 'Status', value: entityStatusLabel(entity) },
        { label: 'Created', value: relativeTime(entity.created_at) },
        { label: 'Last updated', value: relativeTime(entity.updated_at) },
        { label: 'Type', value: 'Credential vault' }
      ];
    case 'memory-stores':
      return [
        { label: 'Status', value: entityStatusLabel(entity) },
        { label: 'Created', value: relativeTime(entity.created_at) },
        { label: 'Last updated', value: relativeTime(entity.updated_at) },
        { label: 'Type', value: 'Memory store' }
      ];
  }
}

export function deploymentTrigger(deployment: DeploymentApiResponse) {
  if (!deployment.schedule) {
    return 'Manual';
  }
  if (deployment.schedule && typeof deployment.schedule === 'object') {
    const schedule = deployment.schedule as Record<string, unknown>;
    if (schedule.type === 'cron') {
      return 'Scheduled';
    }
  }
  return 'Manual';
}

export function deploymentAgentVersion(deployment: DeploymentApiResponse) {
  const directVersion = (deployment as { agent_version?: unknown }).agent_version;
  if (typeof directVersion === 'number') {
    return directVersion;
  }
  const agent = objectRecord(deployment.agent);
  const version = agent.version;
  return typeof version === 'number' ? version : null;
}

export function triggerLabel(trigger: unknown) {
  const triggerRecord = objectRecord(trigger);
  return String(triggerRecord.type || 'manual');
}

export function deploymentRunStatus(run: DeploymentRunApiResponse) {
  if (run.error) {
    return 'Failed';
  }
  if (run.session_id) {
    return 'Succeeded';
  }
  return 'Running';
}

export function sectionPathSegment(section: ManagedEntitySection) {
  switch (section) {
    case 'credential-vaults':
      return 'vaults';
    case 'memory-stores':
      return 'memory-stores';
    default:
      return section;
  }
}

export function environmentEditValues(entity: EnvironmentApiResponse): EnvironmentEditValues {
  const config = objectRecord(entity.config);
  const networking = objectRecord(config.networking);
  const packages = environmentPackageRows(config.packages);
  const metadata = objectRecord((entity as EnvironmentApiResponse & { metadata?: unknown }).metadata);
  return {
    name: entity.name,
    description: entity.description || '',
    networkType: networking.type === 'limited' ? 'limited' : 'unrestricted',
    packages: packages.length ? packages : [{ manager: 'pip', value: '' }],
    metadataRows: Object.entries(metadata).length ? Object.entries(metadata).map(([key, value]) => ({ key, value: String(value) })) : [{ key: '', value: '' }]
  };
}

export function environmentPackageRows(packages: unknown): EnvironmentPackageRow[] {
  const record = objectRecord(packages);
  const rows: EnvironmentPackageRow[] = [];
  for (const manager of ['apt', 'cargo', 'gem', 'go', 'npm', 'pip']) {
    const values = Array.isArray(record[manager]) ? record[manager] : [];
    for (const value of values) {
      if (typeof value === 'string' && value.trim()) {
        rows.push({ manager, value });
      }
    }
  }
  return rows;
}

export function environmentConfigBody(values: EnvironmentEditValues) {
  const packages: Record<string, string[] | string> = { type: 'packages', apt: [], cargo: [], gem: [], go: [], npm: [], pip: [] };
  for (const row of values.packages) {
    const manager = row.manager;
    if (!['apt', 'cargo', 'gem', 'go', 'npm', 'pip'].includes(manager)) {
      continue;
    }
    const list = packages[manager];
    if (Array.isArray(list)) {
      const entries = row.value.split(/\s+/).map((value) => value.trim()).filter(Boolean);
      list.push(...entries);
    }
  }
  return {
    type: 'cloud',
    packages,
    networking: values.networkType === 'limited' ? { type: 'limited', allowed_hosts: [], allow_mcp_servers: false, allow_package_managers: false } : { type: 'unrestricted' }
  };
}

export function environmentMetadataBody(values: EnvironmentEditValues) {
  const metadata: Record<string, string> = {};
  for (const row of values.metadataRows) {
    const key = row.key.trim();
    if (key) {
      metadata[key] = row.value;
    }
  }
  return metadata;
}

export function credentialFormValues(credential?: VaultCredentialApiResponse): CredentialFormValues {
  const auth = objectRecord(credential?.auth);
  const authType = auth.type === 'environment_variable' ? 'environment_variable' : 'static_bearer';
  return {
    displayName: credential?.display_name || '',
    authType,
    mcpServerUrl: typeof auth.mcp_server_url === 'string' ? auth.mcp_server_url : '',
    token: '',
    secretName: typeof auth.secret_name === 'string' ? auth.secret_name : '',
    secretValue: ''
  };
}

export function credentialAuthBody(values: CredentialFormValues, includeImmutable: boolean) {
  if (values.authType === 'environment_variable') {
    return {
      type: 'environment_variable',
      ...(includeImmutable ? { secret_name: values.secretName.trim() } : {}),
      secret_value: values.secretValue,
      networking: { type: 'unrestricted' }
    };
  }
  return {
    type: 'static_bearer',
    ...(includeImmutable ? { mcp_server_url: values.mcpServerUrl.trim() } : {}),
    token: values.token
  };
}

export function credentialAuthLabel(auth: unknown) {
  const record = objectRecord(auth);
  if (record.type === 'environment_variable') {
    return 'Environment variable';
  }
  if (record.type === 'mcp_oauth') {
    return 'MCP OAuth';
  }
  return 'Static bearer';
}

export function columnWidth(section: ManagedEntitySection, column: string) {
  if (!column) {
    return 'w-[48px]';
  }
  if (column === 'ID') {
    return 'w-[190px]';
  }
  if (column === 'Status') {
    return 'w-[120px]';
  }
  if (column === 'Created' || column === 'Updated at') {
    return 'w-[150px]';
  }
  if (section === 'deployments' && column === 'Agent') {
    return 'w-[220px]';
  }
  if (column === 'Type' || column === 'Trigger') {
    return 'w-[120px]';
  }
  return '';
}
