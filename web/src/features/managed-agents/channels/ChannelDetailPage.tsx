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
import { toast } from '../../../shared/ui/sonner';
import { errorMessage } from '../utils';
import { channelApi, channelVaultIds, channelResourceValues, type Channel, type ChannelCheck } from './api';
import { ChannelStatusBadge } from './ChannelStatusBadge';
import { PairingsDialog } from './PairingsDialog';
import { channelDetailPath, channelEditPath, channelSessionsTabPath, channelsListPath } from './paths';

import { ChannelSessionsPanel } from './ChannelSessionsPanel';
import { listChannelEnvironments } from './channelEnvironments';
import { useChannelResources } from './channelResources';
import feishuLogo from './feishu.ico';

export function ChannelDetailPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const params = useParams({ strict: false }) as { agentId?: string; channelId?: string };
  const { activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  const agentId = params.agentId ?? '';
  const channelId = params.channelId ?? '';
  const tab = channelDetailTab(location.searchStr);
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) selectWorkspace(routeWorkspaceId);
  }, [routeWorkspaceId, activeWorkspaceId, selectWorkspace]);
  return (
    <ChannelDetailContent
      workspaceId={workspaceId}
      agentId={agentId}
      channelId={channelId}
      tab={tab}
      onTabChange={(next) => {
        const href =
          next === 'sessions'
            ? channelSessionsTabPath(workspaceId, agentId, channelId)
            : channelDetailPath(workspaceId, agentId, channelId);
        void navigate({ href, replace: true });
      }}
      onNavigate={(href) => void navigate({ href })}
    />
  );
}

function ChannelExecutionSummary({ channel }: { channel: Channel }) {
  const { msg } = useI18n();
  const mode = channel.identity_resolution?.mode;
  const connection = channel.connection_status || (channel.connected ? 'healthy' : 'unknown');
  return (
    <>
      <DetailRow label={msg('managedAgents.channels.executionIdentity', '执行身份')}>
        {channel.identity_id || '—'}
        {mode ? ` · ${mode}` : ''}
      </DetailRow>
      <DetailRow label={msg('managedAgents.channels.connectionStatus', '连接状态')}>
        {connection}
        {channel.entry_closed ? ` · ${msg('managedAgents.channels.entryClosed', '入口已关闭')}` : ''}
      </DetailRow>
    </>
  );
}

function channelDetailTab(search: string): 'basic' | 'sessions' {
  const raw = search.startsWith('?') ? search.slice(1) : search;
  return new URLSearchParams(raw).get('tab') === 'sessions' ? 'sessions' : 'basic';
}

export function ChannelDetailContent({
  workspaceId,
  agentId,
  channelId,
  tab,
  onTabChange,
  onNavigate,
}: {
  workspaceId: string;
  agentId: string;
  channelId: string;
  tab: 'basic' | 'sessions';
  onTabChange: (tab: 'basic' | 'sessions') => void;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const client = useQueryClient();
  const [pairingOpen, setPairingOpen] = useState(false);
  const query = useQuery({
    queryKey: ['channel-detail', workspaceId, agentId, channelId],
    enabled: Boolean(agentId && channelId),
    queryFn: async () => {
      const [list, settings, agent, environments] = await Promise.all([
        channelApi<{ data: Channel[] }>(agentId, workspaceId),
        channelApi<{ environment_id: string | null }>(agentId, workspaceId, '/settings'),
        import('../api').then((api) => api.retrieveAgent(agentId, workspaceId).catch(() => null)),
        listChannelEnvironments(workspaceId).catch(() => []),
      ]);
      const environmentName =
        environments.find((environment) => environment.id === settings.environment_id)?.name || settings.environment_id;
      return {
        channel: list.data.find((item) => item.id === channelId) ?? null,
        environmentId: settings.environment_id,
        environmentName,
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
    onError: (error) => {
      toast.error(errorMessage(error));
    },
  });

  const channel = query.data?.channel;
  const connectionCheck = useMutation({
    mutationFn: async () => {
      const checked = await channelApi<ChannelCheck>(agentId, workspaceId, `/${channelId}/check`);
      await query.refetch({ throwOnError: true });
      return checked;
    },
    onSuccess: (checked) => {
      if (checked.ok)
        toast.message(msg('managedAgents.channels.preflightPassed', '连接与配置预检通过；尚未执行模型或沙箱任务'));
      else
        toast.error(
          checked.checks
            .filter((item) => !item.ok)
            .map((item) => item.message)
            .join('；'),
        );
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  return (
    <section className="min-w-0 text-foreground">
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
                    {query.data?.agentName} ·{' '}
                    {channelVersionLabel(
                      channel.agent_version,
                      msg('managedAgents.channels.latestVersion', '默认最新版本'),
                    )}
                  </dd>
                </div>
              </dl>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" disabled={connectionCheck.isPending} onClick={() => connectionCheck.mutate()}>
                {msg('managedAgents.channels.connectionCheck', '连接检测')}
              </Button>
              <Button variant="outline" disabled={toggle.isPending} onClick={() => toggle.mutate(!channel.enabled)}>
                {channel.enabled
                  ? msg('managedAgents.channels.deactivate', '停用')
                  : msg('managedAgents.channels.activate', '启用')}
              </Button>
              <Button onClick={() => onNavigate(channelEditPath(workspaceId, agentId, channelId))}>
                {msg('managedAgents.channels.editAction', '编辑')}
              </Button>
            </div>
          </div>
          <Tabs value={tab} onValueChange={(value) => onTabChange(value === 'sessions' ? 'sessions' : 'basic')}>
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
                  {msg('managedAgents.channels.identitySummary', '群聊 Bot · 单聊 Bot')}
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
                <ChannelExecutionSummary channel={channel} />
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
                  {channelVersionLabel(
                    channel.agent_version,
                    msg('managedAgents.channels.keepLatest', '保持最新（仅对新会话生效）'),
                  )}
                </DetailRow>
                <DetailRow label={msg('managedAgents.channels.columnEnvironment', '环境')}>
                  {query.data?.environmentName || '—'}
                </DetailRow>
                <ChannelMountedResources workspaceId={workspaceId} channel={channel} />
              </DetailCard>
            </TabsContent>
            <TabsContent value="sessions" className="mt-4">
              <ChannelSessionsPanel workspaceId={workspaceId} agentId={agentId} channel={channel} />
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

function DetailCard({ title, onEdit, children }: { title: string; onEdit: () => void; children: ReactNode }) {
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

function ChannelMountedResources({ workspaceId, channel }: { workspaceId: string; channel: Channel }) {
  const { msg } = useI18n();
  const resources = useChannelResources(workspaceId);
  const none = msg('managedAgents.channels.notMounted', '不挂载');
  return (
    <>
      <DetailRow label={msg('managedAgents.channels.mountedMemory', '挂载 Memory')}>
        {resources.data?.memoryStores.find((item) => item.id === channel.memory_store_id)?.label ||
          channel.memory_store_id ||
          none}
      </DetailRow>
      <DetailRow label={msg('managedAgents.channels.mountedVault', '挂载 Vault')}>
        {channelVaultIds(channel)
          .map((id) => resources.data?.vaults.find((item) => item.id === id)?.label || id)
          .join('、') || none}
      </DetailRow>
      <DetailRow label={msg('managedAgents.channels.mountedFiles', '挂载文件')}>
        {channelResourceValues(channel)
          .filter((resource) => resource.type === 'file')
          .map((resource) => `${resource.file_id} · ${resource.mount_path || ''}`)
          .join('、') || none}
      </DetailRow>
      <DetailRow label={msg('managedAgents.channels.mountedGit', '挂载 Git 仓库')}>
        {channelResourceValues(channel)
          .filter((resource) => resource.type === 'github_repository')
          .map((resource) => resource.url)
          .join('、') || none}
      </DetailRow>
    </>
  );
}

function channelVersionLabel(version: number | null | undefined, latest: string) {
  return version ? `v${version}` : latest;
}
