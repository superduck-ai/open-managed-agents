import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { localizedEntityStatusLabel } from '../resources/model';
import type { SessionApiResponse } from '../types';
import { errorMessage } from '../utils';
import type { Channel } from './api';
import {
  channelSessionActor,
  channelSessionChat,
  channelSessionPageSize,
  channelSessionVersion,
  formatChannelTimestamp,
  loadChannelSessions,
  type ChannelSessionStats,
} from './channelSessions';

export function ChannelSessionsPanel({
  workspaceId,
  agentId,
  channel,
}: {
  workspaceId: string;
  agentId: string;
  channel: Channel;
}) {
  const { msg } = useI18n();
  const [pageIndex, setPageIndex] = useState(1);
  const query = useQuery({
    queryKey: ['channel-sessions', workspaceId, agentId, channel.id, channel.app_id ?? ''],
    queryFn: () => loadChannelSessions(agentId, workspaceId, channel),
  });
  const sessions = query.data?.sessions ?? [];
  const pageCount = Math.max(1, Math.ceil(sessions.length / channelSessionPageSize));
  const page = Math.min(pageIndex, pageCount);
  const visible = sessions.slice((page - 1) * channelSessionPageSize, page * channelSessionPageSize);
  const stats = query.data?.stats;

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <StatCard
          title={msg('managedAgents.channels.sessionTotal', 'Session 总数')}
          value={query.isPending || !stats ? '—' : String(stats.total)}
          hint={msg('managedAgents.channels.sessionTotalHint', '当前渠道的全部会话')}
        />
        <StatCard
          title={msg('managedAgents.channels.sourceUsers', '来源用户')}
          value={sourceUserStat(stats, query.isPending)}
          hint={msg(
            'managedAgents.channels.sourceUsersHint',
            '按 open_id、actor_id 或 sender_id 去重；没有用户标识时显示 —',
          )}
        />
        <StatCard
          title={msg('managedAgents.channels.lastActive', '最近活跃')}
          value={query.isPending || !stats ? '—' : formatChannelTimestamp(stats.lastActive)}
          hint={msg('managedAgents.channels.lastActiveHint', '最近一条会话活动时间')}
          compact={Boolean(stats?.lastActive)}
        />
      </div>
      {query.error ? (
        <Alert variant="destructive">
          <AlertDescription>
            {errorMessage(query.error)}
            <Button variant="outline" size="sm" className="ml-3" onClick={() => void query.refetch()}>
              {msg('managedAgents.channels.retry', '重试')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {query.data?.truncated ? (
        <Alert>
          <AlertDescription>
            {msg('managedAgents.channels.sessionsTruncated', '仅展示最近读取到的会话，更早的会话未全部加载。')}
          </AlertDescription>
        </Alert>
      ) : null}
      <div className="overflow-hidden rounded-xl border border-border bg-card">
        {query.isPending ? (
          <div className="border-b border-border px-4 py-3 text-sm text-muted-foreground">
            {msg('managedAgents.channels.sessionSyncing', '会话数据同步中')}
          </div>
        ) : null}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('managedAgents.channels.sessionNameId', 'Session 名称 / ID')}</TableHead>
              <TableHead>{msg('managedAgents.channels.sessionStatus', '状态')}</TableHead>
              <TableHead>{msg('managedAgents.channels.agentVersion', '智能体版本')}</TableHead>
              <TableHead>{msg('managedAgents.channels.sessionSource', '会话来源')}</TableHead>
              <TableHead>{msg('managedAgents.channels.sourceUser', '来源用户 / 飞书 Open ID')}</TableHead>
              <TableHead>{msg('managedAgents.channels.createdAt', '创建时间')}</TableHead>
              <TableHead>{msg('managedAgents.channels.updatedAt', '更新时间')}</TableHead>
              <TableHead>{msg('managedAgents.channels.columnActions', '操作')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isPending ? (
              <TableRow>
                <TableCell colSpan={8} className="py-10 text-center text-sm text-muted-foreground">
                  {msg('managedAgents.channels.sessionsLoading', '正在加载会话…')}
                </TableCell>
              </TableRow>
            ) : query.error ? null : visible.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="py-10 text-center text-sm text-muted-foreground">
                  {msg('managedAgents.channels.sessionEmpty', '暂无会话')}
                </TableCell>
              </TableRow>
            ) : (
              visible.map((session) => (
                <ChannelSessionRow key={session.id} session={session} workspaceId={workspaceId} />
              ))
            )}
          </TableBody>
        </Table>
        {query.isPending || query.error ? null : (
          <div className="flex items-center justify-end gap-3 border-t border-border px-4 py-3 text-sm text-muted-foreground">
            <span>{msg('managedAgents.channels.sessionCount', '共 {count} 条', { count: sessions.length })}</span>
            <Button
              type="button"
              variant="outline"
              size="icon"
              disabled={page <= 1}
              aria-label={msg('pagination.previousPage', '上一页')}
              onClick={() => setPageIndex(page - 1)}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <span className="inline-flex h-8 min-w-8 items-center justify-center rounded-md border border-border px-2 text-foreground">
              {page}
            </span>
            <Button
              type="button"
              variant="outline"
              size="icon"
              disabled={page >= pageCount}
              aria-label={msg('pagination.nextPage', '下一页')}
              onClick={() => setPageIndex(page + 1)}
            >
              <ChevronRight className="size-4" />
            </Button>
            <span>{msg('managedAgents.channels.sessionPageSize', '20 条/页')}</span>
          </div>
        )}
      </div>
    </div>
  );
}

function ChannelSessionRow({ session, workspaceId }: { session: SessionApiResponse; workspaceId: string }) {
  const { msg } = useI18n();
  const actor = channelSessionActor(session);
  const chat = channelSessionChat(session);
  const source =
    chat === 'direct'
      ? `${msg('managedAgents.channels.sessionSourceFeishu', '飞书')} · ${msg('managedAgents.channels.sessionChatDirect', '单聊')}`
      : chat === 'group'
        ? `${msg('managedAgents.channels.sessionSourceFeishu', '飞书')} · ${msg('managedAgents.channels.sessionChatGroup', '群聊')}`
        : msg('managedAgents.channels.sessionSourceFeishu', '飞书');
  return (
    <TableRow>
      <TableCell>
        <div className="max-w-[240px]">
          <div className="truncate text-sm text-foreground">{session.title || '—'}</div>
          <div className="truncate font-mono text-xs text-muted-foreground" title={session.id}>
            {session.id}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <span className="inline-flex items-center gap-1.5 text-sm text-foreground">
          <span className={`size-1.5 rounded-full ${sessionDotClass(session)}`} aria-hidden />
          {localizedEntityStatusLabel('sessions', session, msg)}
        </span>
      </TableCell>
      <TableCell>{channelSessionVersion(session)}</TableCell>
      <TableCell>{source}</TableCell>
      <TableCell>
        {actor.kind === 'user' ? (
          <span className="font-mono text-xs">{actor.id}</span>
        ) : actor.kind === 'scope' ? (
          <div className="max-w-[220px]">
            <div className="truncate font-mono text-xs" title={actor.id}>
              {actor.id}
            </div>
            <div className="text-xs text-muted-foreground">
              {msg('managedAgents.channels.sessionScope', '会话范围')}
            </div>
          </div>
        ) : (
          '—'
        )}
      </TableCell>
      <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
        {formatChannelTimestamp(session.created_at)}
      </TableCell>
      <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
        {formatChannelTimestamp(session.updated_at)}
      </TableCell>
      <TableCell>
        <ButtonLink
          href={`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(session.id)}`}
          variant="link"
          className="h-auto px-0"
        >
          {msg('managedAgents.channels.viewSession', '查看详情')}
        </ButtonLink>
      </TableCell>
    </TableRow>
  );
}

function sourceUserStat(stats: ChannelSessionStats | undefined, pending: boolean) {
  if (pending || !stats) return '—';
  if (stats.sourceUsers === null) return '—';
  return String(stats.sourceUsers);
}

function sessionDotClass(session: SessionApiResponse) {
  if (session.archived_at) return 'bg-muted-foreground';
  if (session.status === 'running' || session.status === 'idle' || session.status === 'active') return 'bg-success';
  if (session.status === 'rescheduling') return 'bg-amber-500';
  return 'bg-muted-foreground';
}

function StatCard({
  title,
  value,
  hint,
  compact = false,
}: {
  title: string;
  value: string;
  hint: string;
  compact?: boolean;
}) {
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <p className="text-sm text-muted-foreground">{title}</p>
      <p className={compact ? 'mt-2 text-base font-semibold tabular-nums' : 'mt-2 text-3xl font-semibold tabular-nums'}>
        {value}
      </p>
      <p className="mt-1 text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}
