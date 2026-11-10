import { useQuery } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { ArrowUpRight, Plug, Plus, Search, Settings } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { productIcons } from '../../../shared/icons/product-icons';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { useWorkspace } from '../../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../../shared/workspaces/presentation';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import {
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '../../../shared/ui/data-table-interactions';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { ResourcePageHeader } from '../../../shared/ui/resource-page-header';
import { ResourceFilterDropdown, ResourceSearchField } from '../../../shared/ui/resource-list-controls';
import { ResourceListPagination } from '../../../shared/ui/resource-list-pagination';
import { ResourceListState } from '../../../shared/ui/resource-list-state';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { toast } from '../../../shared/ui/sonner';
import { errorMessage } from '../utils';
import feishuLogo from './feishu.ico';
import { ChannelStatusBadge } from './ChannelStatusBadge';
import { channelCreatePath, channelDetailPath, channelEditPath } from './paths';
import { channelConnectionStatus, channelConnectionStatusLabel, type ChannelConnectionStatus } from './status';
import { listChannelEnvironments } from './channelEnvironments';
import { listWorkspaceChannels, type WorkspaceChannel } from './workspaceChannels';

const channelListPageSize = 10;
const statusFilters = ['connected', 'pending_test', 'abnormal', 'disabled'] as const;

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
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<ChannelConnectionStatus | 'all'>('all');
  const [filterOpen, setFilterOpen] = useState(false);
  const [pageIndex, setPageIndex] = useState(1);
  const query = useQuery({
    queryKey: ['workspace-channels', workspaceId],
    queryFn: () => listWorkspaceChannels(workspaceId),
    refetchInterval: 15000,
  });
  const environments = useQuery({
    queryKey: ['channel-environment-options', workspaceId],
    queryFn: () => listChannelEnvironments(workspaceId),
  });
  const rows = useMemo(
    () => filterChannelRows(query.data?.channels ?? [], { agentFilter, statusFilter, search }),
    [query.data, agentFilter, statusFilter, search],
  );
  const pageCount = Math.max(1, Math.ceil(rows.length / channelListPageSize));
  const page = Math.min(pageIndex, pageCount);
  const visibleRows = rows.slice((page - 1) * channelListPageSize, page * channelListPageSize);
  const environmentLabel = (environmentId: string | null) =>
    environments.data?.find((environment) => environment.id === environmentId)?.name || environmentId || '—';
  const statusOptions = [
    { value: 'all' as const, label: msg('managedAgents.channels.statusAll', '全部状态') },
    ...statusFilters.map((status) => ({ value: status, label: channelConnectionStatusLabel(status, msg) })),
  ];
  const filtered = Boolean(search.trim()) || statusFilter !== 'all';

  return (
    <section className="min-w-0 text-foreground" aria-label={msg('managedAgents.channels.title', '渠道')}>
      <ResourcePageHeader
        title={msg('managedAgents.channels.title', '渠道')}
        actions={
          <Button size="lg" onClick={() => onNavigate(channelCreatePath(workspaceId, agentFilter))}>
            <Plus className="size-4" />
            {msg('managedAgents.channels.addChannel', '新增渠道')}
          </Button>
        }
      />
      <div className="mb-7 flex flex-wrap items-center gap-2">
        <ResourceSearchField
          id="channels-search"
          value={search}
          placeholder={msg('managedAgents.channels.searchNamePlaceholder', '搜索渠道名称')}
          onChange={(value) => {
            setSearch(value);
            setPageIndex(1);
          }}
        />
        <ResourceFilterDropdown
          label={msg('managedAgents.channels.statusFilter', '连通状态')}
          valueLabel={statusOptions.find((option) => option.value === statusFilter)!.label}
          options={statusOptions}
          value={statusFilter}
          menu="status"
          open={filterOpen}
          menuWidthClass="w-40"
          onOpenChange={(open) => setFilterOpen(Boolean(open))}
          onSelect={(value) => {
            setStatusFilter(value);
            setPageIndex(1);
            setFilterOpen(false);
          }}
        />
      </div>
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
      {query.error ? (
        <Alert variant="destructive">
          <AlertDescription>
            {errorMessage(query.error)}{' '}
            <Button variant="ghost" size="sm" onClick={() => void query.refetch()}>
              {msg('managedAgents.channels.retry', '重试')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : query.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : (
        <>
          <ChannelListTable
            rows={visibleRows}
            workspaceId={workspaceId}
            environmentLabel={environmentLabel}
            onNavigate={onNavigate}
          />
          {rows.length === 0 ? <ChannelListEmpty filtered={filtered} /> : null}
          <ResourceListPagination
            currentPage={page}
            totalPages={pageCount}
            canPrevious={page > 1}
            canNext={page < pageCount}
            onPrevious={() => setPageIndex(page - 1)}
            onNext={() => setPageIndex(page + 1)}
          />
        </>
      )}
    </section>
  );
}

function filterChannelRows(
  channels: WorkspaceChannel[],
  filters: { agentFilter?: string; statusFilter: ChannelConnectionStatus | 'all'; search: string },
) {
  const needle = filters.search.trim().toLowerCase();
  return channels.filter((row) => {
    if (filters.agentFilter && row.agentId !== filters.agentFilter) return false;
    if (filters.statusFilter !== 'all' && channelConnectionStatus(row) !== filters.statusFilter) return false;
    if (!needle) return true;
    return row.name.toLowerCase().includes(needle) || (row.app_id ?? '').toLowerCase().includes(needle);
  });
}

function ChannelListEmpty({ filtered }: { filtered: boolean }) {
  const { msg } = useI18n();
  return (
    <ResourceListState
      icon={filtered ? Search : productIcons.channels}
      title={
        filtered
          ? msg('managedAgents.channels.noMatches', '没有匹配的渠道')
          : msg('managedAgents.channels.emptyTitle', '暂无渠道')
      }
      body={
        filtered
          ? msg('managedAgents.channels.noMatchesHelp', '试试其他名称，或调整筛选。')
          : msg('managedAgents.channels.listEmpty', '暂无渠道。点击「新增渠道」开始接入飞书。')
      }
    />
  );
}

function ChannelListTable({
  rows,
  workspaceId,
  environmentLabel,
  onNavigate,
}: {
  rows: WorkspaceChannel[];
  workspaceId: string;
  environmentLabel: (environmentId: string | null) => string;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const columns = [
    msg('managedAgents.channels.columnChannel', '渠道'),
    msg('managedAgents.channels.columnApp', '应用'),
    msg('managedAgents.channels.columnStatus', '连通状态'),
    msg('managedAgents.channels.columnAgent', '智能体'),
    msg('managedAgents.channels.columnEnvironment', '环境'),
    msg('managedAgents.channels.createdAt', '创建时间'),
  ];
  return (
    <Table className={`${dataTableClassName} min-w-[960px]`}>
      <colgroup>
        <col className="w-44" />
        <col className="w-44" />
        <col className="w-28" />
        <col />
        <col className="w-40" />
        <col className="w-28" />
        <col className="w-16" />
      </colgroup>
      <TableHeader>
        <TableRow className={dataTableHeaderRowClassName}>
          {columns.map((label) => (
            <TableHead key={label} className={dataTableHeaderCellClassName}>
              {label}
            </TableHead>
          ))}
          <TableHead className="w-16">
            <span className="sr-only">{msg('managedAgents.channels.columnActions', '操作')}</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <ChannelListRow
            key={`${row.agentId}:${row.id}`}
            row={row}
            workspaceId={workspaceId}
            environmentLabel={environmentLabel(row.environmentId)}
            onNavigate={onNavigate}
          />
        ))}
      </TableBody>
    </Table>
  );
}

function ChannelListRow({
  row,
  workspaceId,
  environmentLabel,
  onNavigate,
}: {
  row: WorkspaceChannel;
  workspaceId: string;
  environmentLabel: string;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const format = useFormatters();
  const detailHref = channelDetailPath(workspaceId, row.agentId, row.id);
  const editHref = channelEditPath(workspaceId, row.agentId, row.id);
  const openDetail = () => onNavigate(detailHref);
  return (
    <DataTableRow clickable className="group [&>td]:h-11" onClick={openDetail}>
      <DataTableCell edge="start" className="max-w-44 truncate">
        <a
          href={detailHref}
          className="rounded-sm font-medium underline-offset-4 hover:underline focus-visible:underline"
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            openDetail();
          }}
        >
          {row.name}
        </a>
      </DataTableCell>
      <DataTableCell>
        <div className="flex min-w-0 items-start gap-2">
          <img src={feishuLogo} alt="" className="mt-0.5 size-4" width={16} height={16} />
          <div className="min-w-0">
            <div className="truncate">{msg('managedAgents.channels.feishu', '飞书')}</div>
            <div className="truncate font-mono text-xs text-muted-foreground">{row.app_id || '—'}</div>
          </div>
        </div>
      </DataTableCell>
      <DataTableCell>
        <ChannelStatusBadge channel={row} />
      </DataTableCell>
      <DataTableCell>
        <div className="min-w-0">
          <div className="truncate">{row.agentName}</div>
          <div className="mt-1 truncate text-xs text-muted-foreground">
            {msg('managedAgents.channels.latestVersion', '默认最新版本')}
          </div>
        </div>
      </DataTableCell>
      <DataTableCell className="max-w-40 truncate">{environmentLabel}</DataTableCell>
      <DataTableCell className="whitespace-nowrap text-muted-foreground">
        {channelCreatedLabel(row.created_at, format.date)}
      </DataTableCell>
      <DataTableCell edge="end" className="px-2" onClick={(event) => event.stopPropagation()}>
        <div className="flex justify-end">
          <DropdownMenu>
            <DropdownMenuTrigger render={<MoreActionsButton label={msg('common.moreActions', '更多操作')} />} />
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuItem onClick={openDetail}>
                <ArrowUpRight className="size-4" />
                {msg('managedAgents.channels.details', '详情')}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onNavigate(editHref)}>
                <Settings className="size-4" />
                {msg('managedAgents.channels.configure', '配置')}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => reportConnectionCheck(row, msg)}>
                <Plug className="size-4" />
                {msg('managedAgents.channels.connectionCheck', '连接检测')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </DataTableCell>
    </DataTableRow>
  );
}

function channelCreatedLabel(
  value: string | undefined,
  formatDate: (value: string, options?: Intl.DateTimeFormatOptions) => string,
) {
  if (!value || !Number.isFinite(Date.parse(value))) return '—';
  return formatDate(value, { month: 'short', day: 'numeric' });
}

function reportConnectionCheck(row: WorkspaceChannel, msg: ReturnType<typeof useI18n>['msg']) {
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
    toast.message(msg('managedAgents.channels.connectionCheckPending', '渠道尚未完成绑定，请先完成飞书授权或凭据配置'));
    return;
  }
  toast.error(msg('managedAgents.channels.connectionCheckFail', '连接检测异常：桥接报告未连通'));
}
