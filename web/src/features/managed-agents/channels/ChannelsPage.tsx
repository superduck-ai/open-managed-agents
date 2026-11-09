import { useQuery } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { Bot, Plus, Search } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { useWorkspace } from '../../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../../shared/workspaces/presentation';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { ResourcePageHeader } from '../../../shared/ui/resource-page-header';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { toast } from '../../../shared/ui/sonner';
import { errorMessage } from '../utils';
import feishuLogo from './feishu.ico';
import { ChannelHero } from './ChannelHero';
import { ChannelStatusBadge } from './ChannelStatusBadge';
import { channelCreatePath, channelDetailPath, channelEditPath } from './paths';
import { channelConnectionStatus, type ChannelConnectionStatus } from './status';
import { listWorkspaceChannels, type WorkspaceChannel } from './workspaceChannels';

type SearchField = 'name' | 'app_id';

const statusOptions: ChannelConnectionStatus[] = ['connected', 'pending_test', 'abnormal', 'disabled'];

export function ChannelsPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const { activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [routeWorkspaceId, activeWorkspaceId, selectWorkspace]);
  return <ChannelsPageContent workspaceId={workspaceId} onNavigate={(href) => void navigate({ href })} />;
}

export function ChannelsPageContent({
  workspaceId,
  onNavigate,
  agentFilter,
}: {
  workspaceId: string;
  onNavigate: (href: string) => void;
  agentFilter?: string;
}) {
  const { msg } = useI18n();
  const [searchField, setSearchField] = useState<SearchField>('name');
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<ChannelConnectionStatus | 'all'>('all');
  const query = useQuery({
    queryKey: ['workspace-channels', workspaceId],
    queryFn: () => listWorkspaceChannels(workspaceId),
    refetchInterval: 15000,
  });
  const rows = useMemo(() => {
    const data = query.data?.channels ?? [];
    return data.filter((row) => {
      if (agentFilter && row.agentId !== agentFilter) return false;
      if (statusFilter !== 'all' && channelConnectionStatus(row) !== statusFilter) return false;
      const needle = search.trim().toLowerCase();
      if (!needle) return true;
      if (searchField === 'app_id') return (row.app_id ?? '').toLowerCase().includes(needle);
      return row.name.toLowerCase().includes(needle);
    });
  }, [query.data, agentFilter, statusFilter, search, searchField]);

  const runConnectionCheck = (row: WorkspaceChannel) => {
    const status = channelConnectionStatus(row);
    if (status === 'connected') {
      toast.success(msg('managedAgents.channels.connectionCheckOk', '连接检测通过：渠道已连通'));
      return;
    }
    if (status === 'disabled') {
      toast.message(msg('managedAgents.channels.connectionCheckDisabled', '渠道已停用，请先启用后再检测'));
      return;
    }
    if (status === 'pending_test') {
      toast.message(
        msg('managedAgents.channels.connectionCheckPending', '渠道尚未完成绑定，请先完成飞书授权或凭据配置'),
      );
      return;
    }
    toast.error(msg('managedAgents.channels.connectionCheckFail', '连接检测异常：桥接报告未连通'));
  };

  return (
    <section
      className="mx-auto w-full max-w-[1200px] px-6 py-6"
      aria-label={msg('managedAgents.channels.title', '渠道')}
    >
      <ResourcePageHeader
        title={msg('managedAgents.channels.title', '渠道')}
        description={msg(
          'managedAgents.channels.pageDescription',
          '管理智能体在飞书、钉钉、企业微信和微信等企业沟通中的接入渠道',
        )}
        actions={
          <Button onClick={() => onNavigate(channelCreatePath(workspaceId, agentFilter))}>
            <Plus className="size-4" />
            {msg('managedAgents.channels.addChannel', '新增渠道')}
          </Button>
        }
      />
      <ChannelHero />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={searchField}
            onValueChange={(value) => setSearchField((value as SearchField) || 'name')}
            items={[
              { value: 'name', label: msg('managedAgents.channels.searchByName', '名称') },
              { value: 'app_id', label: msg('managedAgents.channels.searchByAppId', '应用 ID') },
            ]}
          >
            <SelectTrigger className="h-9 w-[110px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="name">{msg('managedAgents.channels.searchByName', '名称')}</SelectItem>
              <SelectItem value="app_id">{msg('managedAgents.channels.searchByAppId', '应用 ID')}</SelectItem>
            </SelectContent>
          </Select>
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-9 w-[240px] pl-9"
              value={search}
              placeholder={
                searchField === 'app_id'
                  ? msg('managedAgents.channels.searchAppIdPlaceholder', '搜索应用 ID')
                  : msg('managedAgents.channels.searchNamePlaceholder', '搜索渠道名称')
              }
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          <Select
            value={statusFilter}
            onValueChange={(value) => setStatusFilter((value as ChannelConnectionStatus | 'all') || 'all')}
            items={[
              { value: 'all', label: msg('managedAgents.channels.statusAll', '全部状态') },
              ...statusOptions.map((status) => ({
                value: status,
                label: statusLabel(status, msg),
              })),
            ]}
          >
            <SelectTrigger className="h-9 w-[140px]">
              <SelectValue placeholder={msg('managedAgents.channels.statusFilter', '连通状态')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{msg('managedAgents.channels.statusAll', '全部状态')}</SelectItem>
              {statusOptions.map((status) => (
                <SelectItem key={status} value={status}>
                  {statusLabel(status, msg)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      {query.error ? (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>
            {errorMessage(query.error)}
            <Button variant="outline" size="sm" className="ml-3" onClick={() => void query.refetch()}>
              {msg('managedAgents.channels.retry', '重试')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {query.data?.incomplete ? (
        <Alert className="mb-4">
          <AlertDescription>
            {msg('managedAgents.channels.listIncomplete', '部分智能体的渠道暂时无法读取，列表可能不完整。')}
          </AlertDescription>
        </Alert>
      ) : null}
      {query.data?.truncated ? (
        <Alert className="mb-4">
          <AlertDescription>
            {msg('managedAgents.channels.listTruncated', '活跃智能体超过单次可聚合的上限，列表已截断。')}
          </AlertDescription>
        </Alert>
      ) : null}
      <div className="overflow-hidden rounded-xl border border-border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('managedAgents.channels.columnChannel', '渠道')}</TableHead>
              <TableHead>{msg('managedAgents.channels.columnApp', '应用')}</TableHead>
              <TableHead>{msg('managedAgents.channels.columnStatus', '连通状态')}</TableHead>
              <TableHead>{msg('managedAgents.channels.columnAgent', '智能体')}</TableHead>
              <TableHead>Environment</TableHead>
              <TableHead>{msg('managedAgents.channels.columnActions', '操作')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isPending ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                  {msg('managedAgents.channels.loading', '正在加载渠道…')}
                </TableCell>
              </TableRow>
            ) : rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                  {msg('managedAgents.channels.listEmpty', '暂无渠道。点击「新增渠道」开始接入飞书。')}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow key={`${row.agentId}:${row.id}`}>
                  <TableCell>
                    <button
                      type="button"
                      className="text-sm font-medium text-primary hover:underline"
                      onClick={() => onNavigate(channelDetailPath(workspaceId, row.agentId, row.id))}
                    >
                      {row.name}
                    </button>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2 text-sm">
                      <img src={feishuLogo} alt="" className="size-4" width={16} height={16} />
                      <span>{msg('managedAgents.channels.feishu', '飞书')}</span>
                      <span className="font-mono text-xs text-muted-foreground">{row.app_id || '—'}</span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <ChannelStatusBadge channel={row} />
                  </TableCell>
                  <TableCell>
                    <div className="inline-flex max-w-[240px] items-center gap-2 rounded-full border border-border bg-muted/40 px-2.5 py-1 text-xs">
                      <Bot className="size-3.5 shrink-0 text-muted-foreground" />
                      <span className="truncate">{row.agentName}</span>
                      <span className="shrink-0 text-muted-foreground">
                        {msg('managedAgents.channels.latestVersion', '默认最新版本')}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="inline-flex items-center rounded-full border border-border bg-muted/40 px-2.5 py-1 font-mono text-xs">
                      {row.environmentId || '—'}
                    </span>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-3 text-sm">
                      <button
                        type="button"
                        className="text-primary hover:underline"
                        onClick={() => onNavigate(channelDetailPath(workspaceId, row.agentId, row.id))}
                      >
                        {msg('managedAgents.channels.details', '详情')}
                      </button>
                      <button
                        type="button"
                        className="text-primary hover:underline"
                        onClick={() => onNavigate(channelEditPath(workspaceId, row.agentId, row.id))}
                      >
                        {msg('managedAgents.channels.configure', '配置')}
                      </button>
                      <button
                        type="button"
                        className="text-primary hover:underline"
                        onClick={() => runConnectionCheck(row)}
                      >
                        {msg('managedAgents.channels.connectionCheck', '连接检测')}
                      </button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

function statusLabel(status: ChannelConnectionStatus, msg: ReturnType<typeof useI18n>['msg']) {
  switch (status) {
    case 'connected':
      return msg('managedAgents.channels.statusConnected', '已连通');
    case 'pending_test':
      return msg('managedAgents.channels.statusPendingTest', '待测试');
    case 'abnormal':
      return msg('managedAgents.channels.statusAbnormal', '异常');
    case 'disabled':
      return msg('managedAgents.channels.statusDisabled', '已停用');
  }
}
