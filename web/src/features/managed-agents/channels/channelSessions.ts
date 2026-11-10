import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { sessionVersionLabel } from '../agents/model';
import type { PageCursor, SessionApiResponse } from '../types';
import type { Channel } from './api';

export const channelSessionPageSize = 20;

const sessionPageLimit = 100;
const maxSessionPages = 20;
const bridgeSources = new Set(['lark-channel-bridge', 'lark-channel', 'feishu', 'lark']);
const actorKeys = ['open_id', 'actor_id', 'sender_id'] as const;
const directChatTypes = new Set(['p2p', 'dm', 'direct', 'user', 'private']);
const groupChatTypes = new Set(['group', 'chat', 'topic']);

export type ChannelSessionActor = { kind: 'user'; id: string } | { kind: 'scope'; id: string } | { kind: 'unknown' };

export type ChannelSessionStats = {
  total: number;
  sourceUsers: number | null;
  lastActive: string | null;
};

export type ChannelSessionSnapshot = {
  sessions: SessionApiResponse[];
  stats: ChannelSessionStats;
  truncated: boolean;
};

export type ChannelSessionChat = 'direct' | 'group' | null;

export function channelSessionMetadata(session: SessionApiResponse) {
  const metadata: Record<string, string> = {};
  for (const [key, value] of Object.entries(session.metadata ?? {})) {
    if (typeof value !== 'string') continue;
    const trimmed = value.trim();
    if (trimmed) metadata[key] = trimmed;
  }
  return metadata;
}

export function isBridgeSession(session: SessionApiResponse) {
  const metadata = channelSessionMetadata(session);
  return Boolean(metadata.bridge_binding_id) || bridgeSources.has(metadata.source);
}

export function sessionBelongsToChannel(session: SessionApiResponse, channel: Pick<Channel, 'id' | 'app_id'>) {
  if (!isBridgeSession(session)) return false;
  const metadata = channelSessionMetadata(session);
  if (metadata.channel_id) return metadata.channel_id === channel.id;
  const appId = channel.app_id?.trim() ?? '';
  return Boolean(metadata.app_id && appId && metadata.app_id === appId);
}

export function selectChannelSessions(sessions: SessionApiResponse[], channel: Pick<Channel, 'id' | 'app_id'>) {
  return sessions
    .filter((session) => sessionBelongsToChannel(session, channel))
    .sort((left, right) => sessionTimestamp(right) - sessionTimestamp(left));
}

export function channelSessionActor(session: SessionApiResponse): ChannelSessionActor {
  const metadata = channelSessionMetadata(session);
  for (const key of actorKeys) {
    const id = metadata[key];
    if (id) return { kind: 'user', id };
  }
  if (metadata.scope_id) return { kind: 'scope', id: metadata.scope_id };
  return { kind: 'unknown' };
}

export function channelSessionSourceKind(session: SessionApiResponse): ChannelSessionChat | 'feishu' {
  return channelSessionChat(session) ?? 'feishu';
}

export function channelSessionChat(session: SessionApiResponse): ChannelSessionChat {
  const metadata = channelSessionMetadata(session);
  const raw = (metadata.chat_type || metadata.conversation_type || metadata.scope_type || '').toLowerCase();
  if (directChatTypes.has(raw)) return 'direct';
  if (groupChatTypes.has(raw)) return 'group';
  return null;
}

export function channelSessionVersion(session: SessionApiResponse) {
  const label = sessionVersionLabel(session);
  return label === '-' ? '—' : label.toUpperCase();
}

export function summarizeChannelSessions(sessions: SessionApiResponse[]): ChannelSessionStats {
  const users = new Set<string>();
  let lastActive: string | null = null;
  let lastStamp = Number.NEGATIVE_INFINITY;
  for (const session of sessions) {
    const actor = channelSessionActor(session);
    if (actor.kind === 'user') users.add(actor.id);
    const value = session.updated_at || session.created_at;
    const stamp = Date.parse(value);
    if (Number.isFinite(stamp) && stamp >= lastStamp) {
      lastStamp = stamp;
      lastActive = value;
    }
  }
  return {
    total: sessions.length,
    sourceUsers: sessions.length === 0 || users.size > 0 ? users.size : null,
    lastActive,
  };
}

export function formatChannelTimestamp(
  value: string | null,
  formatDate: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) => string,
) {
  if (!value || !Number.isFinite(Date.parse(value))) return '—';
  return formatDate(value, { year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

export async function loadChannelSessions(
  agentId: string,
  workspaceId: string,
  channel: Pick<Channel, 'id' | 'app_id'>,
): Promise<ChannelSessionSnapshot> {
  const sessions: SessionApiResponse[] = [];
  let cursor: PageCursor = null;
  let truncated = false;
  for (let pageIndex = 0; pageIndex < maxSessionPages; pageIndex += 1) {
    const params: Record<string, unknown> = {
      limit: sessionPageLimit,
      agent_id: agentId,
      statuses: ['rescheduling', 'running', 'idle', 'terminated'],
      include_archived: true,
    };
    if (cursor) params.page = cursor;
    const page = await anthropicBetaApi.sessions.list<SessionApiResponse>(params, workspaceId);
    sessions.push(...(page.data ?? []));
    cursor = page.next_page ?? null;
    if (!cursor) break;
    truncated = pageIndex + 1 >= maxSessionPages;
  }
  const matched = selectChannelSessions(sessions, channel);
  return { sessions: matched, stats: summarizeChannelSessions(matched), truncated };
}

function sessionTimestamp(session: SessionApiResponse) {
  const stamp = Date.parse(session.updated_at || session.created_at);
  return Number.isFinite(stamp) ? stamp : 0;
}
