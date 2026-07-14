import { type CreateAgentInput } from '../types';
import { toRecord } from '../utils';

export function quickstartVaultIdsFromInput(input: Record<string, unknown>) {
  if (Array.isArray(input.vault_ids)) {
    return input.vault_ids
      .filter((id): id is string => typeof id === 'string' && Boolean(id.trim()))
      .map((id) => id.trim());
  }
  if (typeof input.vault_id === 'string' && input.vault_id.trim()) {
    return [input.vault_id.trim()];
  }
  if (typeof input.id === 'string' && input.id.trim()) {
    return [input.id.trim()];
  }
  return [];
}

export function quickstartVaultLabelsFromInput(input: Record<string, unknown>) {
  if (Array.isArray(input.vault_names)) {
    const names = input.vault_names
      .filter((name): name is string => typeof name === 'string' && Boolean(name.trim()))
      .map((name) => name.trim());
    if (names.length) {
      return names;
    }
  }
  if (Array.isArray(input.vaults)) {
    const names = input.vaults
      .map((vault) => toRecord(vault))
      .map((vault) => {
        if (!vault) {
          return '';
        }
        if (typeof vault.display_name === 'string' && vault.display_name.trim()) {
          return vault.display_name.trim();
        }
        if (typeof vault.name === 'string' && vault.name.trim()) {
          return vault.name.trim();
        }
        return typeof vault.id === 'string' ? vault.id.trim() : '';
      })
      .filter(Boolean);
    if (names.length) {
      return names;
    }
  }
  return quickstartVaultIdsFromInput(input);
}

export function quickstartMcpServerUrl(agentConfig: CreateAgentInput | null, serverName: string) {
  const servers = Array.isArray(agentConfig?.mcp_servers) ? agentConfig.mcp_servers : [];
  for (const server of servers) {
    const record = toRecord(server);
    if (record?.name === serverName && typeof record.url === 'string') {
      return record.url;
    }
  }
  return '';
}
