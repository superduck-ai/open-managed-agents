import { useQuery } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { ArrowUpRight, Plug, Plus, Search, Settings } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import type { ApiError } from '../../../shared/api/client';
import { productIcons } from '../../../shared/icons/product-icons';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { useWorkspace } from '../../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../../shared/workspaces/presentation';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import {
  DataTableCell,
  DataTableResourceLink,
  DataTableRow,
  MoreActionsButton,
  dataTableActionsColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableMetaColumnClassName,
  dataTablePrimaryColumnClassName,
  dataTableTimeColumnClassName,
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
import { TableSkeletonRows } from '../../../shared/ui/resource-table';
import { RelativeTimeText } from '../../../shared/ui/resource-time-cell';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { toast } from '../../../shared/ui/sonner';
import { errorMessage } from '../utils';
import feishuLogo from './feishu.ico';
import { ChannelStatusBadge } from './ChannelStatusBadge';
import { channelCreatePath, channelDetailPath, channelEditPath } from './paths';
import {
  channelConnectionStatus,
  channelConnectionStatusLabel,
  channelConnectionStatusNotice,
  type ChannelConnectionStatus,
} from './status';
import { listChannelEnvironments } from './channelEnvironments';
import { listWorkspaceChannels, type WorkspaceChannel } from './workspaceChannels';

const channelListPageSize = 10;
const statusFilters = ['connected', 'connecting', 'pending_test', 'abnormal', 'disabled'] as const;

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
}: {
  workspaceId: string;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<ChannelConnectionStatus | 'all'>('all');
  const [filterOpen, setFilterOpen] = useState(false);
  const [pageIndex, setPageIndex] = useState(1);
  const query = useQuery({
    queryKey: ['workspace-channels', workspaceId],
    queryFn: () => listWorkspaceChannels(workspaceId),
    retry: (failureCount, error) => {
      const status = (error as Partial<ApiError>).status;
      if (status === 503 || (status && status >= 400 && status < 500)) return false;
      return failureCount < 1;
    },
    refetchInterval: 15000,
  });
  const environments = useQuery({
    queryKey: ['channel-environment-options', workspaceId],
    queryFn: () => listChannelEnvironments(workspaceId),
  });
  const rows = useMemo(
    () => filterChannelRows(query.data?.channels ?? [], { statusFilter, search }, query.dataUpdatedAt),
    [query.data, query.dataUpdatedAt, statusFilter, search],
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
  const unavailable = !query.data && query.isError && (query.error as Partial<ApiError>).status === 503;

  return (
    <section className="min-w-0 text-foreground" aria-label={msg('managedAgents.channels.title', '渠道')}>
      <ResourcePageHeader
        title={msg('managedAgents.channels.title', '渠道')}
        actions={
          <Button size="lg" onClick={() => onNavigate(channelCreatePath(workspaceId))}>
            <Plus className="size-4" />
            {msg('managedAgents.channels.addChannel', '新增渠道')}
          </Button>
        }
      />
      <div className="mb-7 flex flex-wrap items-center gap-2">
        <ResourceSearchField
          id="channels-search"
          value={search}
          placeholder={msg('managedAgents.common.searchByNameOrId', '按名称或精确 ID 搜索')}
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
      {query.data?.truncated ? (
        <Alert className="mb-4">
          <AlertDescription>
            {msg('managedAgents.channels.listTruncated', '活跃智能体超过单次可聚合的上限，列表已截断。')}
          </AlertDescription>
        </Alert>
      ) : null}
      {query.isError && !unavailable ? (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>
            {errorMessage(query.error)}{' '}
            <Button variant="ghost" size="sm" onClick={() => void query.refetch()}>
              {msg('managedAgents.channels.retry', '重试')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {query.isPending && !query.data ? (
        <ChannelListTable
          rows={[]}
          loading
          workspaceId={workspaceId}
          observedAt={query.dataUpdatedAt}
          environmentLabel={environmentLabel}
          onNavigate={onNavigate}
        />
      ) : query.data || unavailable ? (
        <>
          <ChannelListTable
            rows={visibleRows}
            workspaceId={workspaceId}
            observedAt={query.dataUpdatedAt}
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
      ) : null}
    </section>
  );
}

function filterChannelRows(
  channels: WorkspaceChannel[],
  filters: { statusFilter: ChannelConnectionStatus | 'all'; search: string },
  observedAt: number,
) {
  const needle = filters.search.trim().toLowerCase();
  return channels.filter((row) => {
    if (filters.statusFilter !== 'all' && channelConnectionStatus(row, observedAt) !== filters.statusFilter)
      return false;
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
          : msg('managedAgents.channels.listEmpty', '点击「新增渠道」开始使用。')
      }
    />
  );
}

function ChannelListTable({
  rows,
  loading = false,
  workspaceId,
  observedAt,
  environmentLabel,
  onNavigate,
}: {
  rows: WorkspaceChannel[];
  loading?: boolean;
  workspaceId: string;
  observedAt: number;
  environmentLabel: (environmentId: string | null) => string;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const columns = [
    { label: msg('managedAgents.channels.columnChannel', '渠道'), className: dataTablePrimaryColumnClassName },
    { label: msg('managedAgents.channels.columnApp', '应用'), className: dataTableMetaColumnClassName },
    { label: msg('managedAgents.channels.columnStatus', '连通状态'), className: dataTableMetaColumnClassName },
    { label: msg('managedAgents.channels.columnAgent', '智能体'), className: dataTableMetaColumnClassName },
    { label: msg('managedAgents.channels.columnEnvironment', '环境'), className: dataTableMetaColumnClassName },
    { label: msg('managedAgents.channels.createdAt', '创建时间'), className: dataTableTimeColumnClassName },
  ];
  return (
    <Table className={dataTableClassName} aria-busy={loading}>
      <TableHeader>
        <TableRow className={dataTableHeaderRowClassName}>
          {columns.map((column) => (
            <TableHead key={column.label} className={cn(dataTableHeaderCellClassName, column.className)}>
              {column.label}
            </TableHead>
          ))}
          <TableHead className={cn(dataTableHeaderCellClassName, dataTableActionsColumnClassName)}>
            <span className="sr-only">{msg('managedAgents.channels.columnActions', '操作')}</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? <TableSkeletonRows colCount={columns.length + 1} rowCount={3} /> : null}
        {rows.map((row) => (
          <ChannelListRow
            key={`${row.agentId}:${row.id}`}
            row={row}
            workspaceId={workspaceId}
            observedAt={observedAt}
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
  observedAt,
  environmentLabel,
  onNavigate,
}: {
  row: WorkspaceChannel;
  workspaceId: string;
  observedAt: number;
  environmentLabel: string;
  onNavigate: (href: string) => void;
}) {
  const { msg } = useI18n();
  const detailHref = channelDetailPath(workspaceId, row.agentId, row.id);
  const editHref = channelEditPath(workspaceId, row.agentId, row.id);
  const openDetail = () => onNavigate(detailHref);
  return (
    <DataTableRow clickable className="group" onClick={openDetail}>
      <DataTableCell edge="start" className={dataTablePrimaryColumnClassName}>
        <DataTableResourceLink
          href={detailHref}
          title={row.name}
          className="block max-w-[280px] truncate rounded-sm font-medium text-foreground"
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            openDetail();
          }}
        >
          {row.name}
        </DataTableResourceLink>
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <div className="flex min-w-0 items-start gap-2">
          <img src={feishuLogo} alt="" className="mt-0.5 size-4" width={16} height={16} />
          <div className="min-w-0">
            <div className="truncate">{msg('managedAgents.channels.feishu', '飞书')}</div>
            <div className="font-mono text-xs text-muted-foreground">{row.app_id || '—'}</div>
          </div>
        </div>
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <ChannelStatusBadge channel={row} observedAt={observedAt} />
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <div className="min-w-0 max-w-[240px]">
          <div className="truncate">{row.agentName}</div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            {row.agent_version ? `v${row.agent_version}` : msg('managedAgents.channels.latestVersion', '默认最新版本')}
          </div>
        </div>
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <span title={environmentLabel} className="block max-w-[220px] truncate text-muted-foreground">
          {environmentLabel}
        </span>
      </DataTableCell>
      <DataTableCell className={dataTableTimeColumnClassName}>
        <RelativeTimeText value={row.created_at} />
      </DataTableCell>
      <DataTableCell
        edge="end"
        className={dataTableActionsColumnClassName}
        onClick={(event) => event.stopPropagation()}
      >
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

function reportConnectionCheck(row: WorkspaceChannel, msg: ReturnType<typeof useI18n>['msg']) {
  toast.message(channelConnectionStatusNotice(channelConnectionStatus(row), msg));
}
