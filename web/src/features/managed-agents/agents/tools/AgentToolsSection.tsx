import { useI18n } from '../../../../shared/i18n';
import { Badge } from '../../../../shared/ui/badge';
import { Card } from '../../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../../shared/ui/collapsible';
import { Ban, BriefcaseBusiness, CheckCircle2, ChevronRight, Ellipsis, Hand, Server, Wrench } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { type AgentApiResponse } from '../../types';
import { loadMcpDirectoryServers } from './api';
import {
  buildAgentToolDisplayCards,
  type AgentToolDisplayCard,
  type McpDirectoryServer,
  type ToolPermissionState
} from './model';

export function AgentToolsSection({ agent }: { agent: AgentApiResponse }) {
  const { msg } = useI18n();
  const [directoryState, setDirectoryState] = useState<{
    key: string;
    status: 'idle' | 'ready' | 'failed';
    servers: McpDirectoryServer[];
  }>({ key: '', status: 'idle', servers: [] });
  const mcpServersKey = useMemo(
    () => JSON.stringify(Array.isArray(agent.mcp_servers) ? agent.mcp_servers : []),
    [agent.mcp_servers]
  );
  const hasMcpServers = mcpServersKey !== '[]';
  const directoryStatus = directoryState.key === mcpServersKey
    ? directoryState.status
    : hasMcpServers
      ? 'loading'
      : 'idle';

  useEffect(() => {
    if (!hasMcpServers) {
      return;
    }
    let active = true;
    void loadMcpDirectoryServers()
      .then((servers) => {
        if (active) {
          setDirectoryState({ key: mcpServersKey, status: 'ready', servers });
        }
      })
      .catch(() => {
        if (active) {
          setDirectoryState({ key: mcpServersKey, status: 'failed', servers: [] });
        }
      });
    return () => {
      active = false;
    };
  }, [hasMcpServers, mcpServersKey]);

  const cards = useMemo(
    () => buildAgentToolDisplayCards(
      agent,
      directoryState.key === mcpServersKey ? directoryState.servers : []
    ),
    [agent, directoryState.key, directoryState.servers, mcpServersKey]
  );

  return (
    <div className="space-y-2" aria-busy={directoryStatus === 'loading'}>
      <span className="sr-only" role="status" aria-live="polite">
        {directoryStatus === 'loading'
          ? msg('managedAgents.agents.detail.mcpDirectoryLoading', 'Loading MCP tool metadata.')
          : directoryStatus === 'ready'
            ? msg('managedAgents.agents.detail.mcpDirectoryLoaded', 'MCP tool metadata loaded.')
            : directoryStatus === 'failed'
              ? msg('managedAgents.agents.detail.mcpDirectoryUnavailable', 'MCP tool metadata is unavailable.')
              : ''}
      </span>
      {cards.map((card) => (
        <AgentToolCard key={card.key} card={card} />
      ))}
    </div>
  );
}

export function AgentToolCard({ card }: { card: AgentToolDisplayCard }) {
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
        </div>
      </div>
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <CollapsibleTrigger
          aria-label={[title, triggerLabel, card.tools.length, aggregatePermissionLabel].filter((value) => value !== undefined).join(' ')}
          className="flex h-11 w-full items-center gap-3 border-t border-border px-4 text-left text-sm font-semibold text-muted-foreground transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        >
          <ChevronRight
            className={`size-4 shrink-0 text-muted-foreground/70 transition-transform ${expanded ? 'rotate-90' : ''}`}
            aria-hidden
          />
          {triggerLabel}
          <Badge variant="secondary" className="h-auto rounded-md px-2 py-0.5 text-xs font-normal text-muted-foreground">
            {card.tools.length}
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
              {msg('managedAgents.agents.detail.noToolList', 'No tool list available.')}
            </div>
          )}
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
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
