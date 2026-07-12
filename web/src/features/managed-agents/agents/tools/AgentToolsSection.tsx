import { useI18n } from '../../../../shared/i18n';
import { AuthContext } from '../../../../shared/auth/context';
import { Badge } from '../../../../shared/ui/badge';
import { Button } from '../../../../shared/ui/button';
import { Card } from '../../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../../shared/ui/collapsible';
import { toast } from '../../../../shared/ui/sonner';
import { Ban, BriefcaseBusiness, CheckCircle2, ChevronRight, Ellipsis, Hand, RefreshCw, Server, Wrench } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useContext, useMemo, useState } from 'react';
import { type AgentApiResponse } from '../../types';
import { errorMessage } from '../../utils';
import { loadAgentMcpToolCatalogs, loadMcpDirectoryServers, refreshAgentMcpToolCatalogs } from './api';
import {
  buildAgentToolDisplayCards,
  type AgentToolDisplayCard,
  type McpToolCatalog,
  type ToolPermissionState
} from './model';

type McpCatalogQueryData = { data: McpToolCatalog[]; version: number };
type McpCatalogQueryKey = readonly ['agent-mcp-tool-catalogs', string, string, string, number];
type McpRefreshVariables = {
  orgUuid: string;
  workspaceId: string;
  agentId: string;
  version: number;
  serverName: string;
  queryKey: McpCatalogQueryKey;
};

export function AgentToolsSection({
  agent,
  orgUuid,
  workspaceId
}: {
  agent: AgentApiResponse;
  orgUuid: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const csrfToken = useContext(AuthContext)?.csrfToken;
  const queryClient = useQueryClient();
  const hasMcpServers = Array.isArray(agent.mcp_servers) && agent.mcp_servers.length > 0;
  const catalogEnabled = hasMcpServers && Boolean(orgUuid && workspaceId && agent.id);
  const catalogQueryKey = ['agent-mcp-tool-catalogs', orgUuid, workspaceId, agent.id, agent.version] as const;
  const directoryQuery = useQuery({
    queryKey: ['mcp-directory-servers'],
    queryFn: loadMcpDirectoryServers,
    enabled: hasMcpServers,
    staleTime: 60 * 60 * 1000,
    retry: false
  });
  // 详情页初始加载只读取数据库中最近一次成功快照，不轮询、不自动触发 MCP 探测。
  const catalogQuery = useQuery({
    queryKey: catalogQueryKey,
    queryFn: ({ signal }) => loadAgentMcpToolCatalogs(orgUuid, workspaceId, agent.id, agent.version, signal),
    enabled: catalogEnabled,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    retry: 1
  });
  // 手动刷新会同步等待后端探测并落库；成功响应就是新的权威快照，直接写入 Query 缓存。
  // 探测期间不做乐观替换，因此失败时页面仍保留旧工具列表。
  const refreshMutation = useMutation({
    // 请求作用域作为 mutation variables 固化，避免等待探测时 Agent 切换版本后，
    // 旧 endpoint 的响应被误写进新版本的 Query cache。
    mutationFn: (variables: McpRefreshVariables) =>
      refreshAgentMcpToolCatalogs(
        variables.orgUuid,
        variables.workspaceId,
        variables.agentId,
        variables.version,
        variables.serverName,
        csrfToken
      ),
    onSuccess: async (response, variables) => {
      const current = queryClient.getQueryData<McpCatalogQueryData>(variables.queryKey);
      queryClient.setQueryData<McpCatalogQueryData>(variables.queryKey, () => {
        const catalogs = current?.data ?? [];
        const existingIndex = catalogs.findIndex((catalog) => catalog.server_name === response.data.server_name);
        const nextCatalogs = existingIndex >= 0
          ? catalogs.map((catalog, index) => index === existingIndex ? response.data : catalog)
          : [...catalogs, response.data];
        return { data: nextCatalogs, version: response.version || current?.version || variables.version };
      });

      // 初次 GET 失败时没有可安全 merge 的完整 collection：先展示本次成功结果，
      // 再仅对仍活跃的同一作用域回源 GET，避免其他 MCP 的已保存快照丢失。
      if (!current) {
        await queryClient.invalidateQueries({
          queryKey: variables.queryKey,
          exact: true,
          refetchType: 'active'
        });
      }
    },
    onError: (error) => {
      toast.error(
        msg('managedAgents.agents.detail.refreshMcpToolsFailed', 'Could not refresh MCP tools.'),
        { description: errorMessage(error) }
      );
    }
  });

  const cards = useMemo(
    () => buildAgentToolDisplayCards(
      agent,
      directoryQuery.data ?? [],
      catalogQuery.data?.data ?? []
    ),
    [agent, catalogQuery.data?.data, directoryQuery.data]
  );
  const refreshScopeIsCurrent = Boolean(
    refreshMutation.variables &&
    refreshMutation.variables.orgUuid === orgUuid &&
    refreshMutation.variables.workspaceId === workspaceId &&
    refreshMutation.variables.agentId === agent.id &&
    refreshMutation.variables.version === agent.version
  );
  const refreshPending = refreshMutation.isPending && refreshScopeIsCurrent;
  const refreshSucceeded = refreshMutation.isSuccess && refreshScopeIsCurrent;
  const catalogBusy = catalogQuery.isFetching || refreshPending;
  const catalogStatusMessage = catalogEnabled
    ? refreshPending
      ? msg('managedAgents.agents.detail.refreshMcpToolsPending', 'Refreshing and saving MCP tools.')
      : refreshSucceeded
        ? msg('managedAgents.agents.detail.refreshMcpToolsSucceeded', 'MCP tools refreshed and saved.')
        : catalogQuery.isFetching
          ? msg('managedAgents.agents.detail.mcpCatalogLoading', 'Loading saved MCP tools.')
          : catalogQuery.isSuccess
            ? msg('managedAgents.agents.detail.mcpCatalogLoaded', 'Saved MCP tools loaded.')
            : catalogQuery.isError
              ? msg('managedAgents.agents.detail.mcpCatalogUnavailable', 'Saved MCP tools are unavailable.')
              : ''
    : '';
  // Catalog 和 Directory 是两条独立异步链路；同时组合两者的状态，
  // 避免真实 Console 上 catalog 已启用时屏蔽 Directory 的读屏播报。
  const directoryStatusMessage = directoryQuery.isFetching
    ? msg('managedAgents.agents.detail.mcpDirectoryLoading', 'Loading MCP tool metadata.')
    : directoryQuery.isSuccess
      ? msg('managedAgents.agents.detail.mcpDirectoryLoaded', 'MCP tool metadata loaded.')
      : directoryQuery.isError
        ? msg('managedAgents.agents.detail.mcpDirectoryUnavailable', 'MCP tool metadata is unavailable.')
        : '';
  const asyncStatusMessage = [catalogStatusMessage, directoryStatusMessage].filter(Boolean).join(' ');

  return (
    <div className="space-y-2" aria-busy={catalogBusy || directoryQuery.isFetching}>
      <span className="sr-only" role="status" aria-live="polite">
        {asyncStatusMessage}
      </span>
      {cards.map((card) => (
        <AgentToolCard
          key={card.key}
          card={card}
          refreshBusy={refreshPending && refreshMutation.variables?.serverName === card.serverName}
          refreshDisabled={refreshPending || catalogQuery.isFetching}
          onRefresh={catalogEnabled && card.serverName ? () => refreshMutation.mutate({
            orgUuid,
            workspaceId,
            agentId: agent.id,
            version: agent.version,
            serverName: card.serverName!,
            queryKey: catalogQueryKey
          }) : undefined}
        />
      ))}
    </div>
  );
}

export function AgentToolCard({
  card,
  refreshBusy = false,
  refreshDisabled = false,
  onRefresh
}: {
  card: AgentToolDisplayCard;
  refreshBusy?: boolean;
  refreshDisabled?: boolean;
  onRefresh?: () => void;
}) {
  const { msg } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const title =
    card.kind === 'built-in'
      ? msg('managedAgents.agents.detail.builtInTools', 'Built-in tools')
      : card.kind === 'custom'
        ? msg('managedAgents.agents.detail.customTools', 'Custom tools')
        : card.title;
  const subtitle =
    card.kind === 'custom'
      ? msg('managedAgents.agents.detail.customToolsDescription', 'Client-handled tool definitions')
      : card.subtitle;
  const triggerLabel = card.kind === 'custom'
    ? msg('managedAgents.agents.detail.tools', 'Tools')
    : msg('managedAgents.agents.detail.toolPermissions', 'Tool permissions');
  const aggregatePermissionLabel = card.aggregatePermission
    ? permissionLabel(card.aggregatePermission, msg)
    : undefined;
  const catalogStatusLabel = card.catalogStatus ? mcpCatalogStatusLabel(card.catalogStatus, msg) : undefined;
  const toolCount = card.toolCountKnown === false ? '—' : card.tools.length;

  return (
    <Card size="sm" className="gap-0 py-0">
      <div className="flex min-w-0 items-center gap-3 px-4 py-3">
        <ToolCardIcon card={card} />
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-sm font-semibold text-foreground">{title}</h3>
          {card.kind === 'custom' ? (
            <span className="block truncate text-xs text-muted-foreground">{subtitle}</span>
          ) : (
            <code className="block truncate font-mono text-xs text-muted-foreground">{subtitle}</code>
          )}
          {catalogStatusLabel ? (
            <span className="mt-1 block truncate text-xs text-muted-foreground">
              {catalogStatusLabel}
            </span>
          ) : null}
        </div>
        {card.kind === 'mcp' && onRefresh ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={refreshDisabled}
            onClick={onRefresh}
            aria-label={msg(
              'managedAgents.agents.detail.refreshMcpTools',
              'Refresh MCP tools for {server}',
              { server: title }
            )}
          >
            <RefreshCw className={refreshBusy ? 'animate-spin' : ''} aria-hidden />
            {msg('common.refresh', 'Refresh')}
          </Button>
        ) : null}
      </div>
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <CollapsibleTrigger
          aria-label={[title, triggerLabel, toolCount, aggregatePermissionLabel, catalogStatusLabel].filter((value) => value !== undefined).join(' ')}
          className="flex h-11 w-full items-center gap-3 border-t border-border px-4 text-left text-sm font-semibold text-muted-foreground transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        >
          <ChevronRight
            className={`size-4 shrink-0 text-muted-foreground/70 transition-transform ${expanded ? 'rotate-90' : ''}`}
            aria-hidden
          />
          {triggerLabel}
          <Badge variant="secondary" className="h-auto rounded-md px-2 py-0.5 text-xs font-normal text-muted-foreground">
            {toolCount}
          </Badge>
          <span className="min-w-0 flex-1" />
          {card.aggregatePermission ? <PermissionBadge permission={card.aggregatePermission} /> : null}
        </CollapsibleTrigger>
        <CollapsibleContent className="border-t border-border">
          {card.tools.length ? (
            <div className={card.kind === 'mcp' ? 'subtle-scrollbar max-h-64 divide-y divide-border overflow-y-auto' : 'divide-y divide-border'}>
              {card.tools.map((tool, index) => (
                <div
                  key={`${tool.name}-${index}`}
                  className={`grid min-w-0 items-center gap-x-4 gap-y-1 px-4 py-2.5 text-sm sm:pl-12 ${
                    tool.permission
                      ? 'grid-cols-[minmax(0,1fr)_auto] sm:grid-cols-[10rem_minmax(0,1fr)_auto]'
                      : 'grid-cols-1 sm:grid-cols-[10rem_minmax(0,1fr)]'
                  }`}
                >
                  <code className="min-w-0 truncate font-mono text-xs text-foreground" title={tool.name}>
                    {tool.name}
                  </code>
                  {tool.description ? (
                    <span
                      className={`min-w-0 truncate text-muted-foreground ${
                        tool.permission
                          ? 'col-span-2 row-start-2 sm:col-span-1 sm:col-start-2 sm:row-start-1'
                          : ''
                      }`}
                      title={tool.description}
                    >
                      {tool.description}
                    </span>
                  ) : (
                    <span className="hidden min-w-0 sm:block" />
                  )}
                  {tool.permission ? (
                    <PermissionBadge
                      permission={tool.permission}
                      className="col-start-2 row-start-1 justify-self-end sm:col-start-3"
                    />
                  ) : null}
                </div>
              ))}
            </div>
          ) : (
            <div className="px-4 py-3 pl-12 text-sm text-muted-foreground">
              {card.catalogStatus === 'ready'
                ? msg('managedAgents.agents.detail.noToolsDiscovered', 'This server reported no tools.')
                : msg('managedAgents.agents.detail.noToolList', 'No tool list available.')}
            </div>
          )}
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
}

function mcpCatalogStatusLabel(
  status: NonNullable<AgentToolDisplayCard['catalogStatus']>,
  msg: ReturnType<typeof useI18n>['msg']
) {
  switch (status) {
    case 'unknown':
      return msg('managedAgents.agents.detail.mcpStatusUnknown', 'Tool list not refreshed');
    case 'ready':
      return msg('managedAgents.agents.detail.mcpStatusReady', 'Saved tool list');
    case 'error':
      return msg('managedAgents.agents.detail.mcpStatusError', 'Tool list unavailable');
  }
}

function PermissionBadge({ permission, className = '' }: { permission: ToolPermissionState; className?: string }) {
  const { msg } = useI18n();
  const metadata = {
    always_allow: {
      icon: CheckCircle2,
      className: 'status-success'
    },
    always_ask: {
      icon: Hand,
      className: 'status-warning'
    },
    always_deny: {
      icon: Ban,
      className: 'status-danger'
    },
    custom: {
      icon: Ellipsis,
      className: 'border-border bg-muted text-muted-foreground'
    }
  } satisfies Record<ToolPermissionState, { icon: typeof CheckCircle2; className: string }>;
  const item = metadata[permission];
  const Icon = item.icon;
  return (
    <Badge variant="outline" className={`${item.className} ${className}`}>
      <Icon aria-hidden />
      {permissionLabel(permission, msg)}
    </Badge>
  );
}

function permissionLabel(permission: ToolPermissionState, msg: ReturnType<typeof useI18n>['msg']) {
  switch (permission) {
    case 'always_allow':
      return msg('managedAgents.agents.detail.alwaysAllow', 'Always allow');
    case 'always_ask':
      return msg('managedAgents.agents.detail.alwaysAsk', 'Always ask');
    case 'always_deny':
      return msg('managedAgents.agents.detail.alwaysDeny', 'Always deny');
    case 'custom':
      return msg('managedAgents.agents.detail.customPermission', 'Custom');
  }
}

function ToolCardIcon({ card }: { card: AgentToolDisplayCard }) {
  const [failedIconUrl, setFailedIconUrl] = useState<string>();
  const fallback =
    card.kind === 'built-in' ? (
      <BriefcaseBusiness className="size-5" aria-hidden />
    ) : card.kind === 'custom' ? (
      <Wrench className="size-5" aria-hidden />
    ) : (
      <Server className="size-5" aria-hidden />
    );

  return (
    <span className="grid size-9 shrink-0 place-items-center overflow-hidden rounded-lg border border-border bg-secondary text-foreground">
      {card.iconUrl && failedIconUrl !== card.iconUrl ? (
        <img
          src={card.iconUrl}
          alt=""
          className="size-5 object-contain"
          onError={() => setFailedIconUrl(card.iconUrl)}
        />
      ) : (
        fallback
      )}
    </span>
  );
}
