import { anthropicBetaApi } from '../../../shared/api/anthropic';

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
export const channelApi = anthropicBetaApi.agents.channels;
