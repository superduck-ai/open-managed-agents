import type { gitResourceBody } from '../resources/git-resource';

export type ChannelGitRepository = ReturnType<typeof gitResourceBody> & { tokenConfigured?: boolean };
import { anthropicBetaApi } from '../../../shared/api/anthropic';

export type ChannelResource =
  { type: 'file'; file_id: string; mount_path?: string } | (ChannelGitRepository & { type: 'github_repository' });

export interface Channel {
  resources?: ChannelResource[];
  agent_version?: number | null;
  id: string;
  name: string;
  mode: 'open' | 'pairing';
  source: 'console' | 'api';
  enabled: boolean;
  connected: boolean;
  binding_status: 'bound' | 'unbound';
  memory_store_id?: string | null;
  vault_id?: string | null;
  vault_ids?: string[];
  git_repository?: ChannelGitRepository | null;
  app_id?: string;
  include_thinking: boolean;
  include_tool_calls: boolean;
  reply_in_thread: boolean;
  pending_pairings: number;
  created_at?: string;
  identity_id?: string;
  identity_resolution?: { mode?: 'fixed' | 'pairing' };
  connection_status?: string;
  entry_closed?: boolean;
}
export interface ChannelInput {
  resources?: ChannelResource[];
  agent_version?: number | null;
  name: string;
  mode: Channel['mode'];
  include_thinking: boolean;
  include_tool_calls: boolean;
  reply_in_thread: boolean;
  memory_store_id?: string | null;
  vault_id?: string | null;
  vault_ids?: string[];
  git_repository?: ChannelGitRepository | null;
  app_id?: string;
  app_secret?: string;
}
export interface Pairing {
  id: string;
  kind: 'user' | 'chat';
  target: string;
  name: string;
  status: 'pending' | 'approved' | 'rejected';
}
export interface ChannelQr {
  status: 'pending' | 'bound' | 'expired' | 'error' | 'unknown';
  url: string;
  expires_at: string;
}
export type ChannelAuthorization = ChannelQr & { id: string; app_id?: string };
export const channelApi = anthropicBetaApi.agents.channels;

export type ChannelCheck = {
  ok: boolean;
  execution_verified: boolean;
  checks: { code: string; ok: boolean; message: string }[];
};

export function channelVaultIds(value: { vault_ids?: string[]; vault_id?: string | null }): string[] {
  return value.vault_ids ?? (value.vault_id ? [value.vault_id] : []);
}

export function channelResourceValues(value: {
  resources?: ChannelResource[];
  git_repository?: ChannelGitRepository | null;
}): ChannelResource[] {
  return value.resources ?? (value.git_repository ? [{ ...value.git_repository, type: 'github_repository' }] : []);
}
