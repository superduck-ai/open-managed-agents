import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate, useParams } from '@tanstack/react-router';
import { ArrowLeft, Pencil } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { useWorkspace } from '../../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../../shared/workspaces/presentation';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { toast } from '../../../shared/ui/sonner';
import { errorMessage } from '../utils';
import { channelApi, type Channel } from './api';
import { ChannelStatusBadge } from './ChannelStatusBadge';
import { PairingsDialog } from './PairingsDialog';
import { channelEditPath, channelsListPath } from './paths';
import { channelConnectionStatus } from './status';
import feishuLogo from './feishu.ico';

export function ChannelDetailPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const params = useParams({ strict: false }) as { agentId?: string; channelId?: string };
  const { activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) selectWorkspace(routeWorkspaceId);
  }, [routeWorkspaceId, activeWorkspaceId, selectWorkspace]);
  return (
    <ChannelDetailContent
      workspaceId={workspaceId}
      agentId={params.agentId ?? ''}
      channelId={params.channelId ?? ''}
      onNavigate={(href) => void navigate({ href })}
    />
  );
}

export function ChannelDetailContent({
  workspaceId,
  agentId,
  channelId,
  onNavigate,
}: {
  workspaceId: string;
  agentId: string;
  channelId: string;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const client = useQueryClient();
  const [tab, setTab] = useState('basic');
  const [pairingOpen, setPairingOpen] = useState(false);
  const query = useQuery({
    queryKey: ['channel-detail', workspaceId, agentId, channelId],
    enabled: Boolean(agentId && channelId),
    queryFn: async () => {
      const [list, settings, agent] = await Promise.all([
        channelApi<{ data: Channel[] }>(agentId, workspaceId),
        channelApi<{ environment_id: string | null }>(agentId, workspaceId, '/settings'),
        import('../api').then((api) => api.retrieveAgent(agentId, workspaceId).catch(() => null)),
      ]);
      return {
        channel: list.data.find((item) => item.id === channelId) ?? null,
        environmentId: settings.environment_id,
        agentName: agent?.name ?? agentId,
      };
    },
    refetchInterval: 10000,
  });
  const toggle = useMutation({
    mutationFn: async (enabled: boolean) => {
      if (!query.data?.channel) return;
      return channelApi<Channel>(agentId, workspaceId, `/${channelId}`, 'POST', { enabled });
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['channel-detail', workspaceId, agentId, channelId] });
      await client.invalidateQueries({ queryKey: ['workspace-channels', workspaceId] });
    },
  });

  const channel = query.data?.channel;
  const runConnectionCheck = () => {
    if (!channel) return;
    const status = channelConnectionStatus(channel);
    if (status === 'connected') toast.success(msg('managedAgents.channels.connectionCheckOk', '连接检测通过：渠道已连通'));
    else if (status === 'disabled')
      toast.message(msg('managedAgents.channels.connectionCheckDisabled', '渠道已停用，请先启用后再检测'));
    else if (status === 'pending_test')
      toast.message(
        msg('managedAgents.channels.connectionCheckPending', '渠道尚未完成绑定，请先完成飞书授权或凭据配置'),
      );
    else toast.error(msg('managedAgents.channels.connectionCheckFail', '连接检测异常：桥接报告未连通'));
  };

  return (
    <section className="mx-auto w-full max-w-[1100px] px-6 py-6">
      <button
        type="button"
        className="mb-4 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        onClick={() => onNavigate(channelsListPath(workspaceId))}
      >
        <ArrowLeft className="size-4" />
        {msg('managedAgents.channels.detailPage', '渠道详情页')}
      </button>
      {query.error ? (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>{errorMessage(query.error)}</AlertDescription>
        </Alert>
      ) : null}
      {!query.isPending && !channel ? (
        <Alert className="mb-4">
          <AlertDescription>{msg('managedAgents.channels.notFound', '未找到该渠道')}</AlertDescription>
        </Alert>
      ) : null}
      {channel ? (
        <>
          <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-3">
                <h1 className="text-2xl font-medium">{channel.name}</h1>
                <ChannelStatusBadge channel={channel} />
              </div>
              <dl className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
                <div>
                  <dt className="inline">{msg('managedAgents.channels.channelId', '渠道 ID')}：</dt>
                  <dd className="inline font-mono">{channel.id}</dd>
                </div>
                <div>
                  <dt className="inline">{msg('managedAgents.channels.appIdLabel', '应用 ID')}：</dt>
                  <dd className="inline font-mono">{channel.app_id || '—'}</dd>
                </div>
                <div>
                  <dt className="inline">{msg('managedAgents.channels.columnAgent', '智能体')}：</dt>
                  <dd className="inline">
                    {query.data?.agentName} · {msg('managedAgents.channels.latestVersion', '默认最新版本')}
                  </dd>
                </div>
              </dl>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" onClick={runConnectionCheck}>
                {msg('managedAgents.channels.connectionCheck', '连接检测')}
              </Button>
              <Button
                variant="outline"
                disabled={toggle.isPending}
                onClick={() => toggle.mutate(!channel.enabled)}
              >
                {channel.enabled
                  ? msg('managedAgents.channels.deactivate', '停用')
                  : msg('managedAgents.channels.activate', '启用')}
              </Button>
              <Button onClick={() => onNavigate(channelEditPath(workspaceId, agentId, channelId))}>
                {msg('managedAgents.channels.editAction', '编辑')}
              </Button>
            </div>
          </div>
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList>
              <TabsTrigger value="basic">{msg('managedAgents.channels.basicConfig', '基础配置')}</TabsTrigger>
              <TabsTrigger value="sessions">{msg('managedAgents.channels.sessionMgmt', '会话管理')}</TabsTrigger>
            </TabsList>
            <TabsContent value="basic" className="mt-4 space-y-4">
              <DetailCard
                title={msg('managedAgents.channels.connectApp', '连接应用')}
                onEdit={() => onNavigate(channelEditPath(workspaceId, agentId, channelId))}
              >
                <DetailRow label={msg('managedAgents.channels.channelType', '渠道类型')}>
                  <span className="inline-flex items-center gap-2">
                    <img src={feishuLogo} alt="" className="size-4" width={16} height={16} />
                    {msg('managedAgents.channels.feishu', '飞书')}
                  </span>
                </DetailRow>
                <DetailRow label={msg('managedAgents.channels.channelName', '渠道名称')}>{channel.name}</DetailRow>
              </DetailCard>
              <DetailCard
                title={msg('managedAgents.channels.channelStrategy', '渠道策略')}
                onEdit={() => onNavigate(channelEditPath(workspaceId, agentId, channelId))}
              >
                <DetailRow label={msg('managedAgents.channels.identityType', '身份类型')}>
                  {msg('managedAgents.channels.identitySummary', '群聊 Bot · 单聊 User')}
                </DetailRow>
                <DetailRow label={msg('managedAgents.channels.replyDisplay', '回复展示')}>
                  <div className="space-y-1 text-sm">
                    <div>
                      {msg('managedAgents.channels.thinking', '展示思考过程')}：
                      {channel.include_thinking
                        ? msg('managedAgents.channels.on', '开')
                        : msg('managedAgents.channels.off', '关')}
                    </div>
                    <div>
                      {msg('managedAgents.channels.tools', '展示工具调用')}：
                      {channel.include_tool_calls
                        ? msg('managedAgents.channels.on', '开')
                        : msg('managedAgents.channels.off', '关')}
                    </div>
                  </div>
                </DetailRow>
                <DetailRow label={msg('managedAgents.channels.threadReplyMethod', '话题回复方式')}>
                  {channel.reply_in_thread
                    ? msg('managedAgents.channels.threadReplyDetail', '话题回复（在原始消息下延续讨论）')
                    : msg('managedAgents.channels.mainReply', '主会话回复')}
                </DetailRow>
                <DetailRow label={msg('managedAgents.channels.mode', '渠道模式')}>
                  {channel.mode === 'open'
                    ? msg('managedAgents.channels.open', '开放模式')
                    : msg('managedAgents.channels.pairing', '配对模式')}
                  {channel.mode === 'pairing' ? (
                    <Button variant="link" className="h-auto px-2" onClick={() => setPairingOpen(true)}>
                      {msg('managedAgents.channels.pairings', '配对管理')}
                      {channel.pending_pairings ? ` (${channel.pending_pairings})` : ''}
                    </Button>
                  ) : null}
                </DetailRow>
              </DetailCard>
              <DetailCard
                title={msg('managedAgents.channels.maStrategy', 'MA 策略')}
                onEdit={() => onNavigate(channelEditPath(workspaceId, agentId, channelId))}
              >
                <DetailRow label={msg('managedAgents.channels.columnAgent', '智能体')}>
                  {query.data?.agentName}
                </DetailRow>
                <DetailRow label={msg('managedAgents.channels.agentVersionStrategy', '智能体版本策略')}>
                  {msg('managedAgents.channels.keepLatest', '保持最新（仅对新会话生效）')}
                </DetailRow>
                <DetailRow label="Environment">{query.data?.environmentId || '—'}</DetailRow>
                <DetailRow label={msg('managedAgents.channels.mountedMemory', '挂载 Memory')}>—</DetailRow>
                <DetailRow label={msg('managedAgents.channels.mountedVault', '挂载 Vault')}>—</DetailRow>
              </DetailCard>
            </TabsContent>
            <TabsContent value="sessions" className="mt-4 space-y-4">
              <div className="grid gap-3 sm:grid-cols-3">
                <StatCard
                  title={msg('managedAgents.channels.sessionTotal', 'Session 总数')}
                  value="—"
                  hint={msg('managedAgents.channels.sessionTotalHint', '当前渠道的全部会话')}
                />
                <StatCard
                  title={msg('managedAgents.channels.sourceUsers', '来源用户')}
                  value="—"
                  hint={msg('managedAgents.channels.sourceUsersHint', '按当前应用内的用户标识去重')}
                />
                <StatCard
                  title={msg('managedAgents.channels.lastActive', '最近活跃')}
                  value="—"
                  hint={msg('managedAgents.channels.lastActiveHint', '最近一条会话活动时间')}
                />
              </div>
              <div className="rounded-xl border border-border">
                <div className="border-b border-border px-4 py-3 text-sm text-muted-foreground">
                  {msg('managedAgents.channels.sessionSyncing', '会话数据同步中')}
                </div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Session / ID</TableHead>
                      <TableHead>{msg('managedAgents.channels.sessionStatus', '状态')}</TableHead>
                      <TableHead>{msg('managedAgents.channels.agentVersion', '智能体版本')}</TableHead>
                      <TableHead>{msg('managedAgents.channels.sessionSource', '会话来源')}</TableHead>
                      <TableHead>{msg('managedAgents.channels.sourceUser', '来源用户')}</TableHead>
                      <TableHead>{msg('managedAgents.channels.columnActions', '操作')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableRow>
                      <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                        {msg(
                          'managedAgents.channels.sessionEmpty',
                          '暂无按渠道维度的会话同步接口；桥接会话仍可在「会话」页按智能体查看。',
                        )}
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </div>
            </TabsContent>
          </Tabs>
          {pairingOpen ? (
            <PairingsDialog
              agentId={agentId}
              workspaceId={workspaceId}
              channel={channel}
              canEdit
              finalFocus={() => null}
              onClose={() => setPairingOpen(false)}
            />
          ) : null}
        </>
      ) : null}
    </section>
  );
}

function DetailCard({
  title,
  onEdit,
  children,
}: {
  title: string;
  onEdit: () => void;
  children: ReactNode;
}) {
  return (
    <section className="rounded-xl border border-border bg-card p-5">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 className="text-base font-semibold">{title}</h2>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={title} onClick={onEdit}>
          <Pencil className="size-4" />
        </Button>
      </div>
      <dl className="space-y-3">{children}</dl>
    </section>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[180px_minmax(0,1fr)] sm:items-start">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="text-sm text-foreground">{children}</dd>
    </div>
  );
}

function StatCard({ title, value, hint }: { title: string; value: string; hint: string }) {
  return (
    <div className="rounded-xl border border-border bg-card p-4">
      <p className="text-sm text-muted-foreground">{title}</p>
      <p className="mt-2 text-3xl font-semibold tabular-nums">{value}</p>
      <p className="mt-1 text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}
