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
  type ToolPermissionState
} from './model';

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
  // 这里只轮询后端缓存状态：loading/refreshing 时每秒读取一次，进入终态立即停止；
  // 浏览器不会直连 MCP，也不会在轮询时重复提交 refresh。
  const catalogQuery = useQuery({
    queryKey: catalogQueryKey,
    queryFn: ({ signal }) => loadAgentMcpToolCatalogs(orgUuid, workspaceId, agent.id, agent.version, signal),
    enabled: catalogEnabled,
    refetchInterval: (query) =>
      query.state.data?.data.some((catalog) => catalog.status === 'loading' || catalog.status === 'refreshing')
        ? 1000
        : false,
    refetchIntervalInBackground: true,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    retry: 1
  });
  // Refresh POST 只负责入队或复用 generation；成功后失效 GET query，
  // 再由上面的状态轮询等待异步发现结果。
  const refreshMutation = useMutation({
    mutationFn: (serverName: string) =>
      refreshAgentMcpToolCatalogs(orgUuid, workspaceId, agent.id, agent.version, [serverName], csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: catalogQueryKey });
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
  const catalogBusy = catalogQuery.isFetching || cards.some((card) => card.catalogStatus === 'loading' || card.catalogStatus === 'refreshing');

  return (
    <div className="space-y-2" aria-busy={catalogBusy || directoryQuery.isFetching}>
      <span className="sr-only" role="status" aria-live="polite">
        {catalogEnabled
          ? catalogBusy
            ? msg('managedAgents.agents.detail.mcpCatalogLoading', 'Discovering MCP tools.')
            : catalogQuery.isSuccess
              ? msg('managedAgents.agents.detail.mcpCatalogLoaded', 'MCP tool discovery finished.')
              : catalogQuery.isError
                ? msg('managedAgents.agents.detail.mcpCatalogUnavailable', 'MCP tool discovery is unavailable.')
                : ''
          : directoryQuery.isFetching
            ? msg('managedAgents.agents.detail.mcpDirectoryLoading', 'Loading MCP tool metadata.')
            : directoryQuery.isSuccess
              ? msg('managedAgents.agents.detail.mcpDirectoryLoaded', 'MCP tool metadata loaded.')
              : directoryQuery.isError
                ? msg('managedAgents.agents.detail.mcpDirectoryUnavailable', 'MCP tool metadata is unavailable.')
                : ''}
      </span>
      {cards.map((card) => (
        <AgentToolCard
          key={card.key}
          card={card}
          refreshBusy={refreshMutation.isPending && refreshMutation.variables === card.serverName}
          onRefresh={catalogEnabled && card.serverName ? () => refreshMutation.mutate(card.serverName!) : undefined}
        />
      ))}
    </div>
  );
}

export function AgentToolCard({
  card,
  refreshBusy = false,
  onRefresh
}: {
  card: AgentToolDisplayCard;
  refreshBusy?: boolean;
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
            <span className="mt-1 block truncate text-xs text-muted-foreground" title={card.catalogError?.message}>
              {catalogStatusLabel}
            </span>
          ) : null}
        </div>
        {card.kind === 'mcp' && onRefresh ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={refreshBusy || card.catalogStatus === 'loading' || card.catalogStatus === 'refreshing'}
            onClick={onRefresh}
            aria-label={msg('managedAgents.agents.detail.refreshMcpTools', 'Refresh MCP tools')}
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
              {card.catalogStatus === 'loading' || card.catalogStatus === 'refreshing'
                ? msg('managedAgents.agents.detail.discoveringTools', 'Discovering tools…')
                : card.catalogStatus === 'ready'
                  ? msg('managedAgents.agents.detail.noToolsDiscovered', 'This server reported no tools.')
                  : card.catalogError?.message || msg('managedAgents.agents.detail.noToolList', 'No tool list available.')}
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
      return msg('managedAgents.agents.detail.mcpStatusUnknown', 'Tool list not discovered');
    case 'loading':
      return msg('managedAgents.agents.detail.mcpStatusLoading', 'Discovering tools…');
    case 'ready':
      return msg('managedAgents.agents.detail.mcpStatusReady', 'Live tool list');
    case 'refreshing':
      return msg('managedAgents.agents.detail.mcpStatusRefreshing', 'Refreshing; showing last known tools');
    case 'stale':
      return msg('managedAgents.agents.detail.mcpStatusStale', 'Showing last known tools');
    case 'auth_required':
      return msg('managedAgents.agents.detail.mcpStatusAuthRequired', 'Authentication required for discovery');
    case 'error':
      return msg('managedAgents.agents.detail.mcpStatusError', 'Tool discovery failed');
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
