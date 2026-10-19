import { consoleApi } from '../../../shared/api/client';

export interface Channel {
  id: string;
  name: string;
  mode: 'open' | 'pairing';
  source: 'console' | 'api';
  enabled: boolean;
  connected: boolean;
  binding_status: 'bound' | 'unbound';
  app_id?: string;
  include_thinking: boolean;
  include_tool_calls: boolean;
  reply_in_thread: boolean;
  pending_pairings: number;
}
export interface ChannelInput {
  name: string;
  mode: Channel['mode'];
  include_thinking: boolean;
  include_tool_calls: boolean;
  reply_in_thread: boolean;
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
  status: 'pending' | 'bound' | 'expired' | 'error';
  url: string;
  expires_at: string;
}
export function channelApi<T>(agentId: string, workspaceId: string, suffix = '', method = 'GET', body?: unknown) {
  return consoleApi<T>(`/v1/agents/${encodeURIComponent(agentId)}/channels${suffix}?beta=true`, {
    method,
    headers: { 'x-workspace-id': workspaceId, 'anthropic-beta': 'managed-agents-2026-04-01' },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
}
