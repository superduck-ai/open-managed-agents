import { Bot } from 'lucide-react';
import { type ReactNode } from 'react';
import { relativeTime } from '../agents/AgentsResourcePage';
import { localTimezone } from '../api';
import { CompactChip, StatusPill } from '../components/common';
import {
  type CredentialFormValues,
  type DeploymentApiResponse,
  type DeploymentRunApiResponse,
  type EnvironmentApiResponse,
  type EnvironmentEditValues,
  type EnvironmentPackageRow,
  type I18nMsg,
  type ManagedEntityApiResponse,
  type ManagedEntityFormValues,
  type ManagedEntitySection,
  type MemoryApiResponse,
  type MemoryBranchState,
  type MemoryTreeNode,
  type PageResponse,
  type SessionApiResponse,
  type VaultApiResponse,
  type VaultCredentialApiResponse,
} from '../types';
import { formatBytes, objectRecord, sectionPathSegment, titleCase } from '../utils';

export { sectionPathSegment };

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
  return {
    loading: false,
    error: null,
    data: memoryRowsFromPage(page),
    prefixes: memoryPrefixPathsFromValues(page.prefixes ?? []),
  };
}

export function memoryRowsFromPage(page: PageResponse<MemoryApiResponse>) {
  const rows = (page.data ?? [])
    .map(normalizeMemoryRow)
    .filter((memory): memory is MemoryApiResponse => Boolean(memory));
  const existingPaths = new Set(
    rows.map((memory) => (memory.type === 'memory_prefix' ? normalizeMemoryFolderPath(memory.path) : memory.path)),
  );
  const prefixRows = memoryPrefixPathsFromValues(page.prefixes ?? [])
    .filter((prefix) => !existingPaths.has(prefix))
    .map((prefix) => ({
      id: `prefix:${prefix}`,
      content: null,
      content_size_bytes: 0,
      created_at: '',
      memory_store_id: '',
      path: prefix,
      type: 'memory_prefix' as const,
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
      type: 'memory_prefix' as const,
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

export function loadedMemoryRowsFromBranches(
  rootRows: MemoryApiResponse[],
  branches: Record<string, MemoryBranchState>,
) {
  const rows = [...rootRows.filter((memory) => memory.type === 'memory')];
  for (const branch of Object.values(branches)) {
    rows.push(...branch.data.filter((memory) => memory.type === 'memory'));
  }
  return sortMemoryRows(rows);
}

export function memoryFolderPathsFromBranches(
  rootRows: MemoryApiResponse[],
  branches: Record<string, MemoryBranchState>,
) {
  const paths = new Set(memoryFolderPathsFromRows(rootRows));
  for (const branch of Object.values(branches)) {
    for (const path of memoryFolderPathsFromRows(branch.data)) {
      paths.add(path);
    }
  }
  return [...paths].sort((left, right) => left.localeCompare(right));
}

export function buildMemoryTreeNodes(
  rootRows: MemoryApiResponse[],
  expandedFolders: Set<string>,
  branches: Record<string, MemoryBranchState>,
) {
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
        nodes.push({
          type: 'folder',
          path,
          label: memoryFolderLabel(path),
          depth,
          expanded,
          loading: Boolean(branch?.loading),
          error: branch?.error ?? null,
        });
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

export function memoryPreviewContent(memory: MemoryApiResponse, msg?: I18nMsg) {
  if (memory.content) {
    return memory.content;
  }
  const bytes = memory.content_size_bytes ?? 0;
  if (!bytes) {
    return '';
  }
  const size = formatBytes(bytes);
  return msg
    ? msg('managedAgents.memoryStores.storedAt', '{size} stored at {path}', { size, path: memory.path })
    : `${size} stored at ${memory.path}`;
}

export function memoryFileName(path: string) {
  const trimmed = path.trim().replace(/\/+$/, '');
  const name = trimmed.split('/').filter(Boolean).pop();
  return name || 'memory.txt';
}

export function cellsForEntity(
  section: ManagedEntitySection,
  entity: ManagedEntityApiResponse,
  msg?: I18nMsg,
  formatRelativeTime?: (value: number, unit: Intl.RelativeTimeFormatUnit) => string,
): Record<string, ReactNode> {
  const status = (
    <StatusPill tone={statusPillTone(section, entity)}>
      {msg ? localizedEntityStatusLabel(section, entity, msg) : entityStatusLabel(entity)}
    </StatusPill>
  );
  const created = relativeTime(entity.created_at, formatRelativeTime);

  switch (section) {
    case 'sessions':
      return {
        Name: entityDisplayName(section, entity),
        Status: status,
        Agent: <CompactChip icon={Bot}>{entityAgentLabel(entity)}</CompactChip>,
        Created: created,
      };
    case 'deployments':
      return {
        Name: entityDisplayName(section, entity),
        Status: status,
        Agent: <CompactChip icon={Bot}>{entityAgentLabel(entity)}</CompactChip>,
        Trigger: msg
          ? localizedDeploymentTrigger(entity as DeploymentApiResponse, msg)
          : deploymentTrigger(entity as DeploymentApiResponse),
        Created: created,
      };
    case 'environments':
      return {
        Name: entityDisplayName(section, entity),
        Status: status,
        Type: 'Cloud',
        'Updated at': relativeTime(entity.updated_at, formatRelativeTime),
      };
    case 'credential-vaults':
      return {
        Name: entityDisplayName(section, entity),
        Status: status,
        Created: created,
      };
    case 'memory-stores':
      return {
        Name: entityDisplayName(section, entity),
        Status: status,
        Created: created,
      };
  }
}

export function initialFormValues(
  section: ManagedEntitySection,
  entity?: ManagedEntityApiResponse,
): ManagedEntityFormValues {
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
    memoryStoreIds: entity ? entityMemoryStoreIds(entity) : [],
    fileResources: [],
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

export function localizedEntityStatusLabel(
  section: ManagedEntitySection,
  entity: ManagedEntityApiResponse,
  msg: I18nMsg,
) {
  if (entity.archived_at) {
    return msg('common.archived', 'Archived');
  }
  const status = 'status' in entity ? entity.status : 'state' in entity ? entity.state : 'active';
  const normalized = typeof status === 'string' ? status.toLowerCase() : 'active';
  if (section === 'sessions') {
    switch (normalized) {
      case 'active':
        return msg('managedAgents.sessions.statusActive', 'Active');
      case 'idle':
        return msg('managedAgents.sessions.statusIdle', 'Idle');
      case 'running':
        return msg('managedAgents.sessions.statusRunning', 'Running');
      case 'rescheduling':
        return msg('managedAgents.sessions.statusRescheduling', 'Rescheduling');
      case 'terminated':
        return msg('managedAgents.sessions.statusTerminated', 'Terminated');
    }
  }
  if (normalized === 'active') {
    return msg('common.active', 'Active');
  }
  if (normalized === 'paused') {
    return msg('managedAgents.filters.paused', 'Paused');
  }
  return titleCase(normalized);
}

export function statusPillTone(section: ManagedEntitySection, entity: ManagedEntityApiResponse): 'neutral' | 'success' {
  if (entity.archived_at || section === 'sessions' || section === 'deployments') {
    return 'neutral';
  }
  return 'success';
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
      .map((item) =>
        item && typeof item === 'object' && typeof (item as { text?: unknown }).text === 'string'
          ? (item as { text: string }).text
          : '',
      )
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
      resource &&
      typeof resource === 'object' &&
      typeof (resource as { memory_store_id?: unknown }).memory_store_id === 'string'
        ? (resource as { memory_store_id: string }).memory_store_id
        : null,
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

export function detailRowsForEntity(
  section: ManagedEntitySection,
  entity: ManagedEntityApiResponse,
  msg?: I18nMsg,
  formatRelativeTime?: (value: number, unit: Intl.RelativeTimeFormatUnit) => string,
) {
  const label = (key: string, fallback: string) => (msg ? msg(key, fallback) : fallback);
  const status = msg ? localizedEntityStatusLabel(section, entity, msg) : entityStatusLabel(entity);
  const created = relativeTime(entity.created_at, formatRelativeTime);
  const updated = relativeTime(entity.updated_at, formatRelativeTime);
  switch (section) {
    case 'sessions':
      return [
        { label: label('common.status', 'Status'), value: status },
        { label: label('managedAgents.common.agent', 'Agent'), value: entityAgentLabel(entity) },
        {
          label: label('managedAgents.environments.kindTitle', 'Environment'),
          value: (entity as SessionApiResponse).environment_id || '—',
        },
        {
          label: label('managedAgents.deployments.kindTitle', 'Deployment'),
          value: (entity as SessionApiResponse).deployment_id || '—',
        },
      ];
    case 'deployments':
      return [
        { label: label('common.status', 'Status'), value: status },
        { label: label('managedAgents.common.agent', 'Agent'), value: entityAgentLabel(entity) },
        {
          label: label('managedAgents.environments.kindTitle', 'Environment'),
          value: (entity as DeploymentApiResponse).environment_id || '—',
        },
        {
          label: label('managedAgents.common.trigger', 'Trigger'),
          value: msg
            ? localizedDeploymentTrigger(entity as DeploymentApiResponse, msg)
            : deploymentTrigger(entity as DeploymentApiResponse),
        },
      ];
    case 'environments':
      return [
        { label: label('common.status', 'Status'), value: status },
        { label: label('analytics.table.type', 'Type'), value: label('managedAgents.environments.cloud', 'Cloud') },
        {
          label: label('managedAgents.environments.overview.scope', 'Scope'),
          value: (entity as EnvironmentApiResponse).scope || 'workspace',
        },
        { label: label('common.created', 'Created'), value: created },
      ];
    case 'credential-vaults':
      return [
        { label: label('common.status', 'Status'), value: status },
        { label: label('common.created', 'Created'), value: created },
        { label: label('managedAgents.common.lastUpdated', 'Last updated'), value: updated },
        {
          label: label('analytics.table.type', 'Type'),
          value: label('managedAgents.credentialVaults.kindTitle', 'Vault'),
        },
      ];
    case 'memory-stores':
      return [
        { label: label('common.status', 'Status'), value: status },
        { label: label('common.created', 'Created'), value: created },
        { label: label('managedAgents.common.lastUpdated', 'Last updated'), value: updated },
        {
          label: label('analytics.table.type', 'Type'),
          value: label('managedAgents.memoryStores.kindTitle', 'Memory store'),
        },
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

export function localizedDeploymentTrigger(deployment: DeploymentApiResponse, msg: I18nMsg) {
  return deployment.schedule
    ? msg('managedAgents.deployments.trigger.scheduled', 'Scheduled')
    : msg('managedAgents.deployments.trigger.manual', 'Manual');
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

export function deploymentRunStatus(run: DeploymentRunApiResponse, msg?: I18nMsg) {
  if (run.error) {
    return msg ? msg('managedAgents.deployments.runStatus.failed', 'Failed') : 'Failed';
  }
  if (run.session_id) {
    return msg ? msg('managedAgents.deployments.runStatus.succeeded', 'Succeeded') : 'Succeeded';
  }
  return msg ? msg('managedAgents.deployments.runStatus.running', 'Running') : 'Running';
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
    allowMcpServers: networking.allow_mcp_servers === true,
    allowPackageManagers: networking.allow_package_managers === true,
    allowedHostsText: stringArrayValue(networking.allowed_hosts).join('\n'),
    packages: packages.length ? packages : [{ manager: 'pip', value: '' }],
    metadataRows: Object.entries(metadata).length
      ? Object.entries(metadata).map(([key, value]) => ({ key, value: String(value) }))
      : [{ key: '', value: '' }],
  };
}

function stringArrayValue(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((entry): entry is string => typeof entry === 'string' && entry.trim() !== '');
}

// parseAllowedHostsText 把逗号/换行分隔的输入归一化为去重、保序的 host 数组。
export function parseAllowedHostsText(text: string): string[] {
  const seen = new Set<string>();
  const hosts: string[] = [];
  for (const entry of text.split(/[\s,]+/)) {
    const host = entry.trim();
    if (!host || seen.has(host)) {
      continue;
    }
    seen.add(host);
    hosts.push(host);
  }
  return hosts;
}

export function credentialEnvHostsMissing(values: CredentialFormValues): boolean {
  return (
    values.authType === 'environment_variable' &&
    values.networkType === 'limited' &&
    parseAllowedHostsText(values.allowedHostsText).length === 0
  );
}

export function credentialEnvInjectionMissing(values: CredentialFormValues): boolean {
  return values.authType === 'environment_variable' && !values.injectHeader && !values.injectBody;
}

function credentialNetworkingBody(values: CredentialFormValues) {
  if (values.networkType === 'limited') {
    return {
      type: 'limited' as const,
      allowed_hosts: parseAllowedHostsText(values.allowedHostsText),
    };
  }
  return { type: 'unrestricted' as const };
}

function credentialInjectionLocationBody(values: CredentialFormValues) {
  return {
    header: values.injectHeader,
    body: values.injectBody,
  };
}

function credentialInjectionLocationFromAuth(auth: Record<string, unknown>): {
  injectHeader: boolean;
  injectBody: boolean;
} {
  const location = objectRecord(auth.injection_location);
  // CMA create default / Console: header only when omitted.
  if (Object.keys(location).length === 0) {
    return { injectHeader: true, injectBody: false };
  }
  return {
    injectHeader: location.header !== false,
    injectBody: location.body === true,
  };
}

function credentialNetworkingFromAuth(auth: Record<string, unknown>): {
  networkType: 'limited' | 'unrestricted';
  allowedHostsText: string;
} {
  const networking = objectRecord(auth.networking);
  if (networking.type === 'limited') {
    return {
      networkType: 'limited',
      allowedHostsText: stringArrayValue(networking.allowed_hosts).join('\n'),
    };
  }
  return { networkType: 'unrestricted', allowedHostsText: '' };
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
  const packages: Record<string, string[] | string> = {
    type: 'packages',
    apt: [],
    cargo: [],
    gem: [],
    go: [],
    npm: [],
    pip: [],
  };
  for (const row of values.packages) {
    const manager = row.manager;
    if (!['apt', 'cargo', 'gem', 'go', 'npm', 'pip'].includes(manager)) {
      continue;
    }
    const list = packages[manager];
    if (Array.isArray(list)) {
      const entries = row.value
        .split(/\s+/)
        .map((value) => value.trim())
        .filter(Boolean);
      list.push(...entries);
    }
  }
  return {
    type: 'cloud',
    packages,
    networking:
      values.networkType === 'limited'
        ? {
            type: 'limited',
            allowed_hosts: parseAllowedHostsText(values.allowedHostsText),
            allow_mcp_servers: values.allowMcpServers,
            allow_package_managers: values.allowPackageManagers,
          }
        : { type: 'unrestricted' },
  };
}

export function environmentMetadataBody(values: EnvironmentEditValues) {
  const metadata: Record<string, string> = {};
  for (const row of values.metadataRows) {
    if (row.key.trim()) {
      metadata[row.key] = row.value;
    }
  }
  return metadata;
}

export function emptyCredentialFormValues(): CredentialFormValues {
  return {
    displayName: '',
    authType: 'mcp_oauth',
    mcpServerUrl: '',
    token: '',
    secretName: '',
    secretValue: '',
    networkType: 'limited',
    allowedHostsText: '',
    injectHeader: true,
    injectBody: false,
    refreshToken: '',
    refreshTokenEndpoint: '',
    refreshClientId: '',
    refreshClientSecret: '',
    refreshAuthType: 'none',
    oauthClientId: '',
    oauthClientSecret: '',
  };
}

const emptyCredentialRefreshFields = {
  refreshToken: '',
  refreshTokenEndpoint: '',
  refreshClientId: '',
  refreshClientSecret: '',
  refreshAuthType: 'none' as const,
};

/** Apply a form patch; clearing the access token also drops paste-path refresh fields. */
export function patchCredentialFormValues(
  current: CredentialFormValues,
  patch: Partial<CredentialFormValues>,
): CredentialFormValues {
  const next = { ...current, ...patch };
  if (next.token.trim()) {
    return next;
  }
  return { ...next, ...emptyCredentialRefreshFields };
}

export function parseCredentialAuthType(value: string): CredentialFormValues['authType'] {
  if (value === 'environment_variable' || value === 'mcp_oauth' || value === 'static_bearer') {
    return value;
  }
  return '';
}

/** Resolved auth type for existing credentials; defaults to static_bearer when missing. */
export function parseExistingCredentialAuthType(value: string): Exclude<CredentialFormValues['authType'], ''> {
  const parsed = parseCredentialAuthType(value);
  return parsed || 'static_bearer';
}

/** API display_name when the optional Name field is left blank. */
export function credentialDisplayName(values: CredentialFormValues): string {
  if (values.displayName.trim()) {
    return values.displayName.trim();
  }
  if (values.authType === 'environment_variable' && values.secretName.trim()) {
    return values.secretName.trim();
  }
  if (values.mcpServerUrl.trim()) {
    try {
      return new URL(values.mcpServerUrl.trim()).hostname;
    } catch {
      return values.mcpServerUrl.trim();
    }
  }
  if (values.authType === 'mcp_oauth') {
    return 'MCP OAuth credential';
  }
  if (values.authType === 'environment_variable') {
    return 'Environment variable';
  }
  if (values.authType === 'static_bearer') {
    return 'Static bearer credential';
  }
  return 'Credential';
}

export function credentialFormValues(credential?: VaultCredentialApiResponse): CredentialFormValues {
  if (!credential) {
    return emptyCredentialFormValues();
  }
  const auth = objectRecord(credential.auth);
  const authType = parseExistingCredentialAuthType(typeof auth.type === 'string' ? auth.type : '');
  const networking = authType === 'environment_variable' ? credentialNetworkingFromAuth(auth) : null;
  const injection = authType === 'environment_variable' ? credentialInjectionLocationFromAuth(auth) : null;
  return {
    ...emptyCredentialFormValues(),
    displayName: credential.display_name || '',
    authType,
    mcpServerUrl: typeof auth.mcp_server_url === 'string' ? auth.mcp_server_url : '',
    secretName: typeof auth.secret_name === 'string' ? auth.secret_name : '',
    ...(networking ?? {}),
    ...(injection ?? {}),
  };
}

function credentialRefreshStarted(values: CredentialFormValues) {
  return (
    Boolean(values.refreshToken.trim()) ||
    Boolean(values.refreshTokenEndpoint.trim()) ||
    Boolean(values.refreshClientId.trim()) ||
    Boolean(values.refreshClientSecret.trim()) ||
    values.refreshAuthType !== 'none'
  );
}

function credentialRefreshBody(values: CredentialFormValues) {
  const refreshToken = values.refreshToken.trim();
  const tokenEndpoint = values.refreshTokenEndpoint.trim();
  const clientId = values.refreshClientId.trim();
  if (!refreshToken || !tokenEndpoint || !clientId) {
    return undefined;
  }
  const authType = values.refreshAuthType;
  return {
    token_endpoint: tokenEndpoint,
    client_id: clientId,
    refresh_token: refreshToken,
    token_endpoint_auth:
      authType === 'none'
        ? { type: 'none' as const }
        : { type: authType, client_secret: values.refreshClientSecret.trim() },
  };
}

export function credentialAuthBody(values: CredentialFormValues, mode: 'create' | 'update') {
  if (!values.authType) {
    throw new Error('Credential auth type is required');
  }
  if (values.authType === 'environment_variable') {
    // Keep secret_value verbatim; env values may intentionally include whitespace.
    const secretValue = values.secretValue;
    const body: Record<string, unknown> = {
      type: 'environment_variable',
      networking: credentialNetworkingBody(values),
      injection_location: credentialInjectionLocationBody(values),
    };
    if (mode === 'create') {
      body.secret_name = values.secretName.trim();
      body.secret_value = secretValue;
    } else if (secretValue.trim()) {
      body.secret_value = secretValue;
    }
    return body;
  }
  if (values.authType === 'mcp_oauth') {
    const accessToken = values.token.trim();
    const refresh = mode === 'create' ? credentialRefreshBody(values) : undefined;
    return {
      type: 'mcp_oauth',
      ...(mode === 'create' ? { mcp_server_url: values.mcpServerUrl.trim() } : {}),
      ...(accessToken ? { access_token: accessToken } : {}),
      ...(refresh ? { refresh } : {}),
    };
  }
  const token = values.token.trim();
  const body: Record<string, unknown> = {
    type: 'static_bearer',
    mcp_server_url: values.mcpServerUrl.trim(),
  };
  if (mode === 'create' || token) {
    body.token = token;
  }
  return body;
}

function credentialRefreshComplete(values: CredentialFormValues) {
  if (!credentialRefreshStarted(values)) {
    return true;
  }
  if (!values.refreshToken.trim() || !values.refreshTokenEndpoint.trim() || !values.refreshClientId.trim()) {
    return false;
  }
  if (values.refreshAuthType !== 'none' && !values.refreshClientSecret.trim()) {
    return false;
  }
  return true;
}

export function credentialFormReady(values: CredentialFormValues, mode: 'create' | 'edit', acknowledged: boolean) {
  if (!acknowledged || !values.authType) {
    return false;
  }
  if (values.authType === 'environment_variable') {
    if (credentialEnvInjectionMissing(values) || credentialEnvHostsMissing(values)) {
      return false;
    }
    // Edit: secret value optional (sealed secrets are not returned; rotate only when provided).
    if (mode === 'edit') {
      return true;
    }
    return Boolean(values.secretName.trim() && values.secretValue.trim());
  }
  if (values.authType === 'static_bearer') {
    // Edit: token optional for the same sealed-secret reason (matches main canSubmit).
    if (mode === 'edit') {
      return Boolean(values.mcpServerUrl.trim());
    }
    return Boolean(values.mcpServerUrl.trim() && values.token.trim());
  }
  if (mode === 'create' && !values.mcpServerUrl.trim()) {
    return false;
  }
  if (!credentialRefreshComplete(values)) {
    return false;
  }
  if (mode === 'edit' || values.token.trim()) {
    return true;
  }
  // Connect path: refresh fields belong to paste-token create; client_secret needs client_id
  if (credentialRefreshStarted(values)) {
    return false;
  }
  return !values.oauthClientSecret.trim() || Boolean(values.oauthClientId.trim());
}

export function credentialAuthLabel(auth: unknown, msg?: I18nMsg) {
  const record = objectRecord(auth);
  return credentialAuthTypeLabel(typeof record.type === 'string' ? record.type : '', msg);
}

export function credentialAuthTypeLabel(authType: string, msg?: I18nMsg) {
  if (authType === 'environment_variable') {
    return msg
      ? msg('managedAgents.credentialVaults.credentialDialog.environmentVariable', 'Environment variable')
      : 'Environment variable';
  }
  if (authType === 'mcp_oauth') {
    return msg ? msg('managedAgents.credentialVaults.credentialDialog.mcpOAuth', 'MCP OAuth') : 'MCP OAuth';
  }
  return msg ? msg('managedAgents.credentialVaults.credentialDialog.staticBearer', 'Bearer token') : 'Bearer token';
}

/** Credential display names for vault pickers (CMA-aligned). */
export function vaultCredentialNames(credentials: VaultCredentialApiResponse[], msg: I18nMsg): string[] {
  return credentials.map((credential) => {
    if (typeof credential.display_name === 'string' && credential.display_name.trim()) {
      return credential.display_name.trim();
    }
    const auth = objectRecord(credential.auth);
    if (typeof auth.secret_name === 'string' && auth.secret_name.trim()) {
      return auth.secret_name.trim();
    }
    return credentialAuthLabel(credential.auth, msg);
  });
}

/** Trailing summary for vault pickers: joined names, or “No credentials”. */
export function vaultCredentialSummary(credentials: VaultCredentialApiResponse[], msg: I18nMsg): string {
  const names = vaultCredentialNames(credentials, msg);
  if (!names.length) {
    return msg('managedAgents.credentialVaults.emptyCredentialSummary', 'No credentials');
  }
  return names.join(', ');
}

export function vaultCredentialSummaryFromNames(names: string[], msg: I18nMsg): string {
  if (!names.length) {
    return msg('managedAgents.credentialVaults.emptyCredentialSummary', 'No credentials');
  }
  return names.join(', ');
}

/** Set equality for vault id selections (order-insensitive). */
export function vaultSelectionEquals(left: string[], right: string[]): boolean {
  if (left.length !== right.length) {
    return false;
  }
  const rightIds = new Set(right);
  return left.every((id) => rightIds.has(id));
}

/** Order-insensitive cache key so vault list reshuffles do not restart loads. */
export function vaultCredentialSummaryCacheKey(vaultIds: string[]): string {
  if (!vaultIds.length) {
    return '';
  }
  return [...vaultIds].sort().join('\0');
}

export type VaultCredentialSummaryLoadState =
  { status: 'idle' | 'loading' | 'error'; names?: string[] } | { status: 'ready'; names: string[] };

export type VaultCredentialSummaryCache = {
  workspaceId: string;
  summaries: Record<string, VaultCredentialSummaryLoadState>;
};

/** Active summary map for the current workspace; foreign caches read as empty. */
export function vaultCredentialSummariesForWorkspace(
  cache: VaultCredentialSummaryCache,
  workspaceId: string,
): Record<string, VaultCredentialSummaryLoadState> {
  return cache.workspaceId === workspaceId ? cache.summaries : {};
}

/**
 * Vaults that still need a credential-summary fetch.
 * Includes cancelled in-flight `loading` entries so a restarted effect can recover.
 */
export function vaultCredentialSummaryPendingIds(
  vaultIds: string[],
  summaries: Record<string, VaultCredentialSummaryLoadState | undefined>,
): string[] {
  return vaultIds.filter((vaultId) => {
    const current = summaries[vaultId];
    return !current || current.status !== 'ready';
  });
}

/** Trailing label + tooltip body for a vault row credential summary. */
export function vaultCredentialSummaryPresentation(
  state: VaultCredentialSummaryLoadState | undefined,
  loadingLabel: string,
  msg: I18nMsg,
): { trailing: string; detail: string } {
  const status = state?.status ?? 'idle';
  if (status === 'loading' || status === 'idle') {
    return { trailing: loadingLabel, detail: loadingLabel };
  }
  if (status === 'error') {
    const trailing = vaultCredentialSummaryFromNames([], msg);
    return { trailing, detail: trailing };
  }
  const names = state?.names ?? [];
  const trailing = vaultCredentialSummaryFromNames(names, msg);
  return {
    trailing,
    detail: names.length ? names.join('\n') : trailing,
  };
}

/** CMA-style created label: relative when recent, short calendar date when older. */
export function vaultCreatedLabel(
  createdAt: string,
  formatRelative: (value: number, unit: Intl.RelativeTimeFormatUnit) => string,
  formatDate: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) => string,
): string {
  const timestamp = Date.parse(createdAt);
  if (!Number.isFinite(timestamp)) {
    return '—';
  }
  const seconds = Math.round((timestamp - Date.now()) / 1000);
  if (Math.abs(seconds) < 60) {
    return formatRelative(0, 'second');
  }
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) {
    return formatRelative(minutes, 'minute');
  }
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) {
    return formatRelative(hours, 'hour');
  }
  const days = Math.round(hours / 24);
  if (Math.abs(days) < 7) {
    return formatRelative(days, 'day');
  }
  return formatDate(createdAt, { month: 'short', day: 'numeric' });
}

export function vaultCreatedAbsoluteLabel(
  createdAt: string,
  formatDate: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) => string,
): string {
  const timestamp = Date.parse(createdAt);
  if (!Number.isFinite(timestamp)) {
    return '—';
  }
  return formatDate(createdAt, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZoneName: 'short',
  });
}

/** Map platform vault OAuth error_code wire values to localized user copy. */
export function vaultOAuthErrorMessage(errorCode: string, msg: I18nMsg): string {
  switch (errorCode.trim()) {
    case 'already_exists':
      return msg(
        'managedAgents.credentialVaults.credentialDialog.oauthError.alreadyExists',
        'A credential for this MCP server already exists in the vault.',
      );
    case 'oauth_discovery_failed':
      return msg(
        'managedAgents.credentialVaults.credentialDialog.oauthError.discoveryFailed',
        'Could not discover OAuth settings for this MCP server.',
      );
    case 'token_exchange_failed':
      return msg(
        'managedAgents.credentialVaults.credentialDialog.oauthError.tokenExchangeFailed',
        'OAuth token exchange failed. Try connecting again.',
      );
    case 'verification_request_failed':
      return msg(
        'managedAgents.credentialVaults.credentialDialog.oauthError.verificationFailed',
        'OAuth verification failed. Check the MCP server URL and try again.',
      );
    default:
      return msg(
        'managedAgents.credentialVaults.credentialDialog.oauthError.generic',
        'Could not complete OAuth. Try again.',
      );
  }
}

export function columnWidth(section: ManagedEntitySection, column: string) {
  if (!column) {
    return 'w-[48px]';
  }
  if (section === 'sessions') {
    switch (column) {
      case 'ID':
        return 'w-[180px]';
      case 'Status':
        return 'w-[130px]';
      case 'Agent':
        return 'w-[160px]';
      case 'Tokens in / out':
        return 'w-[140px]';
      case 'Cost':
        return 'w-[130px]';
      case 'Created':
        return 'w-[160px]';
    }
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
