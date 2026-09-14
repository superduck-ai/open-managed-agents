import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import {
  Activity,
  AlertCircle,
  Archive,
  ArrowLeft,
  Copy,
  Eye,
  EyeOff,
  Loader2,
  MoreHorizontal,
  RotateCw,
} from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';

import { useAuth } from '../../shared/auth/context';
import { useFormatters, useI18n } from '../../shared/i18n';
import { copyText } from '../../shared/lib/clipboard';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import { Button } from '../../shared/ui/button';
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '../../shared/ui/card';
import { CopyButton } from '../../shared/ui/copy-button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { ResourceListState } from '../../shared/ui/resource-list-state';
import { Separator } from '../../shared/ui/separator';
import { toast } from '../../shared/ui/sonner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath, workspaceMcpTunnelsPath } from '../../shared/workspaces/presentation';
import {
  archiveMcpTunnel,
  getMcpTunnel,
  probeMcpTunnel,
  revealMcpTunnelToken,
  rotateMcpTunnelToken,
  type McpTunnel,
  type TunnelProbeResult,
} from './api';
import {
  ConfirmTunnelActionDialog,
  CopyValue,
  ProbeDialog,
  TunnelReadinessBadge,
  type PendingTunnelAction,
} from './components';
import { tunnelChannelURL, tunnelClientYaml, tunnelIdFromPath, visibleTunnelRefreshInterval } from './config';

const DEFAULT_LOCAL_MCP_URL = 'http://127.0.0.1:3000/mcp';

export function McpTunnelDetailPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const tunnelId = tunnelIdFromPath(location.pathname);
  const autoReveal = Boolean((location.state as unknown as Record<string, unknown> | undefined)?.mcpTunnelCreated);
  return (
    <McpTunnelDetailContent
      routeWorkspaceId={routeWorkspaceId}
      tunnelId={tunnelId ?? ''}
      autoReveal={autoReveal}
      onNavigate={(href, options) => void navigate({ href, ...options })}
      onAutoRevealConsumed={() => void navigate({ href: location.pathname, replace: true, state: {} })}
    />
  );
}

export function McpTunnelDetailContent({
  routeWorkspaceId,
  tunnelId,
  autoReveal = false,
  onNavigate,
  onAutoRevealConsumed,
}: {
  routeWorkspaceId?: string;
  tunnelId: string;
  autoReveal?: boolean;
  onNavigate?: (href: string, options?: { replace?: boolean }) => void;
  onAutoRevealConsumed?: () => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const queryClient = useQueryClient();
  const { csrfToken } = useAuth();
  const { orgUuid, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  const resourceIdentity = `${orgUuid ?? ''}:${workspaceId}:${tunnelId}`;
  const queryKey = ['console-mcp-tunnel', orgUuid, workspaceId, tunnelId] as const;
  const [revealedToken, setRevealedToken] = useState<{ resourceIdentity: string; value: string } | null>(null);
  const token = revealedToken?.resourceIdentity === resourceIdentity ? revealedToken.value : null;
  const [localMcpUrl, setLocalMcpUrl] = useState(DEFAULT_LOCAL_MCP_URL);
  const [probeResult, setProbeResult] = useState<{ tunnel: McpTunnel; result: TunnelProbeResult } | null>(null);
  const [pendingAction, setPendingAction] = useState<PendingTunnelAction | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const autoRevealStarted = useRef<string | null>(null);

  useEffect(() => {
    setLocalMcpUrl(DEFAULT_LOCAL_MCP_URL);
    setProbeResult(null);
    setPendingAction(null);
    setBusyAction(null);
    setActionError(null);
    autoRevealStarted.current = null;
  }, [resourceIdentity]);

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  const tunnelQuery = useQuery({
    queryKey,
    queryFn: () => getMcpTunnel(orgUuid ?? '', workspaceId, tunnelId),
    enabled: Boolean(orgUuid && workspaceId && tunnelId),
    refetchInterval: visibleTunnelRefreshInterval,
    refetchIntervalInBackground: false,
  });

  const revealToken = useCallback(
    async (tunnel: McpTunnel) => {
      if (!orgUuid || tunnel.archived_at) return;
      setBusyAction('reveal');
      setActionError(null);
      try {
        const revealed = await revealMcpTunnelToken(orgUuid, workspaceId, tunnel.id, csrfToken);
        setRevealedToken({ resourceIdentity, value: revealed.tunnel_token });
      } catch (error) {
        setActionError(
          readableError(error, msg('mcpTunnels.error.generic', 'Something went wrong. Please try again.')),
        );
      } finally {
        setBusyAction(null);
      }
    },
    [csrfToken, msg, orgUuid, resourceIdentity, workspaceId],
  );

  useEffect(() => {
    if (
      !autoReveal ||
      autoRevealStarted.current === resourceIdentity ||
      !tunnelQuery.data ||
      tunnelQuery.data.archived_at
    )
      return;
    autoRevealStarted.current = resourceIdentity;
    onAutoRevealConsumed?.();
    void revealToken(tunnelQuery.data);
  }, [autoReveal, onAutoRevealConsumed, resourceIdentity, revealToken, tunnelQuery.data]);

  const handleProbe = async (tunnel: McpTunnel, channel: string) => {
    if (!orgUuid || tunnel.archived_at) return;
    setBusyAction(`probe:${channel}`);
    setActionError(null);
    try {
      const result = await probeMcpTunnel(orgUuid, workspaceId, tunnel.id, channel, csrfToken);
      setProbeResult({ tunnel, result });
    } catch (error) {
      setActionError(readableError(error, msg('mcpTunnels.error.probeFailed', 'The MCP connection test failed.')));
    } finally {
      setBusyAction(null);
    }
  };

  const handleConfirmedAction = async () => {
    if (!orgUuid || !pendingAction) return;
    const { type, tunnel } = pendingAction;
    setBusyAction(type);
    setActionError(null);
    try {
      if (type === 'rotate') {
        const rotated = await rotateMcpTunnelToken(orgUuid, workspaceId, tunnel.id, csrfToken);
        setRevealedToken({ resourceIdentity, value: rotated.tunnel_token });
        toast.success(msg('mcpTunnels.toast.rotated', 'Tunnel token rotated'));
      } else {
        const archived = await archiveMcpTunnel(orgUuid, workspaceId, tunnel.id, csrfToken);
        setRevealedToken(null);
        queryClient.setQueryData(queryKey, archived);
        toast.success(msg('mcpTunnels.toast.archived', 'MCP tunnel archived'));
      }
      setPendingAction(null);
      await queryClient.invalidateQueries({ queryKey: ['console-mcp-tunnels', orgUuid, workspaceId] });
      await queryClient.invalidateQueries({ queryKey });
    } catch (error) {
      setActionError(readableError(error, msg('mcpTunnels.error.generic', 'Something went wrong. Please try again.')));
    } finally {
      setBusyAction(null);
    }
  };

  if (tunnelQuery.isPending) {
    return <ResourceListState icon={Loader2} title={msg('mcpTunnels.detail.loading', 'Loading tunnel...')} body="" />;
  }

  if (tunnelQuery.isError || !tunnelQuery.data) {
    return (
      <ResourceListState
        icon={AlertCircle}
        title={msg('mcpTunnels.detail.loadFailed', 'Could not load this MCP tunnel')}
        body={readableError(
          tunnelQuery.error,
          msg('mcpTunnels.error.generic', 'Something went wrong. Please try again.'),
        )}
        actionLabel={msg('mcpTunnels.backToList', 'Back to MCP tunnels')}
        onAction={() => onNavigate?.(workspaceMcpTunnelsPath(workspaceId))}
      />
    );
  }

  const tunnel = tunnelQuery.data;
  const yaml = tunnelClientYaml(tunnel, localMcpUrl);
  const archived = Boolean(tunnel.archived_at);
  const channels = tunnel.connection.channels;

  return (
    <section className="mx-auto w-full max-w-6xl space-y-5 text-foreground">
      <Button
        type="button"
        variant="ghost"
        className="-ml-2"
        onClick={() => onNavigate?.(workspaceMcpTunnelsPath(workspaceId))}
      >
        <ArrowLeft className="size-4" aria-hidden />
        {msg('mcpTunnels.backToList', 'Back to MCP tunnels')}
      </Button>

      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-[28px] font-semibold leading-tight">{tunnel.display_name || tunnel.id}</h1>
            <TunnelReadinessBadge tunnel={tunnel} />
          </div>
          <p className="font-mono text-sm text-muted-foreground">{tunnel.id}</p>
          <p className="text-sm text-muted-foreground">
            {msg('mcpTunnels.detail.createdAt', 'Created {date}', {
              date: formatDate(tunnel.created_at, formatters.date),
            })}
          </p>
        </div>
        <TunnelDetailMenu
          tunnel={tunnel}
          busy={Boolean(busyAction)}
          onRotate={() => setPendingAction({ type: 'rotate', tunnel })}
          onArchive={() => setPendingAction({ type: 'archive', tunnel })}
        />
      </header>

      {actionError ? (
        <Alert variant="destructive">
          <AlertCircle aria-hidden />
          <AlertTitle>{msg('mcpTunnels.error.actionFailed', 'Action failed')}</AlertTitle>
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{msg('mcpTunnels.detail.overview', 'Overview')}</CardTitle>
          <CardDescription>
            {msg(
              'mcpTunnels.detail.overviewDescription',
              'Stable identifiers and the main MCP endpoint for this tunnel.',
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <CopyValue label={msg('common.id', 'ID')} value={tunnel.id} />
          <CopyValue label={msg('mcpTunnels.detail.domain', 'Domain')} value={tunnel.domain} />
          <div className="md:col-span-2">
            <CopyValue label={msg('mcpTunnels.secret.canonicalUrl', 'Canonical MCP URL')} value={tunnel.mcp_url} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{msg('mcpTunnels.detail.connectorSetup', 'Connector setup')}</CardTitle>
          <CardDescription>
            {msg(
              'mcpTunnels.detail.connectorSetupDescription',
              'Configure the original tunnel-client to reach a private MCP server.',
            )}
          </CardDescription>
          <CardAction>
            {token ? (
              <Button type="button" variant="outline" size="sm" onClick={() => setRevealedToken(null)}>
                <EyeOff className="size-4" aria-hidden /> {msg('common.hide', 'Hide')}
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={archived || busyAction === 'reveal'}
                onClick={() => void revealToken(tunnel)}
              >
                {busyAction === 'reveal' ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Eye className="size-4" aria-hidden />
                )}
                {msg('mcpTunnels.action.viewToken', 'View token')}
              </Button>
            )}
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="grid gap-2">
            <Label>{msg('mcpTunnels.secret.token', 'Tunnel token')}</Label>
            <div className="flex min-w-0 items-center gap-2 rounded-lg border bg-muted/40 p-2">
              <code className="min-w-0 flex-1 break-all text-xs">{token ?? '••••••••••••••••••••••••'}</code>
              <CopyButton
                value={token ?? ''}
                label={msg('mcpTunnels.copyValue', 'Copy {label}', {
                  label: msg('mcpTunnels.secret.token', 'Tunnel token'),
                })}
                disabled={!token}
              />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="local-mcp-url">{msg('mcpTunnels.secret.localUrl', 'Local MCP server URL')}</Label>
            <Input
              id="local-mcp-url"
              value={localMcpUrl}
              disabled={archived}
              onChange={(event) => setLocalMcpUrl(event.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <div className="flex items-center justify-between gap-3">
              <Label>{msg('mcpTunnels.secret.yaml', 'Tunnel-client YAML')}</Label>
              <CopyButton value={yaml} label={msg('common.copy', 'Copy')} />
            </div>
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-lg bg-muted p-3 text-xs">
              <code>{yaml}</code>
            </pre>
            <p className="text-xs text-muted-foreground">
              {msg('mcpTunnels.secret.envPrefix', 'Set ')}
              <code>OMA_TUNNEL_TOKEN</code>
              {msg('mcpTunnels.secret.envSuffix', ' to the token shown above before starting tunnel-client.')}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{msg('mcpTunnels.detail.connection', 'Connection')}</CardTitle>
          <CardDescription>
            {msg(
              'mcpTunnels.detail.connectionDescription',
              '{instances} connector instances and {channels} live channels.',
              {
                instances: tunnel.connection.instance_count,
                channels: channels.length,
              },
            )}
          </CardDescription>
          <CardAction>
            <TunnelReadinessBadge tunnel={tunnel} />
          </CardAction>
        </CardHeader>
        <CardContent>
          {channels.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{msg('mcpTunnels.probe.channel', 'Channel')}</TableHead>
                  <TableHead>{msg('mcpTunnels.detail.processAffinity', 'Process affinity')}</TableHead>
                  <TableHead>{msg('mcpTunnels.column.instances', 'Instances')}</TableHead>
                  <TableHead>{msg('mcpTunnels.column.mcpUrl', 'MCP URL')}</TableHead>
                  <TableHead className="w-[110px] text-right">{msg('common.actions', 'Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {channels.map((channel) => {
                  const channelURL = tunnelChannelURL(tunnel, channel.name);
                  return (
                    <TableRow key={channel.name}>
                      <TableCell>
                        <code>{channel.name}</code>
                      </TableCell>
                      <TableCell>
                        {channel.process_affinity ? msg('common.yes', 'Yes') : msg('common.no', 'No')}
                      </TableCell>
                      <TableCell>{channel.instance_count}</TableCell>
                      <TableCell>
                        <div className="flex max-w-[400px] items-center gap-1">
                          <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{channelURL}</code>
                          <CopyButton value={channelURL} label={msg('mcpTunnels.action.copyMcpUrl', 'Copy MCP URL')} />
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={archived || busyAction === `probe:${channel.name}`}
                          onClick={() => void handleProbe(tunnel, channel.name)}
                        >
                          {busyAction === `probe:${channel.name}` ? (
                            <Loader2 className="size-4 animate-spin" />
                          ) : (
                            <Activity className="size-4" aria-hidden />
                          )}
                          {msg('mcpTunnels.detail.test', 'Test')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          ) : (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <p className="font-medium">{msg('mcpTunnels.detail.noChannels', 'No live channels')}</p>
              <p className="mt-1 text-sm text-muted-foreground">
                {msg(
                  'mcpTunnels.detail.noChannelsDescription',
                  'Start tunnel-client to publish its configured MCP channels.',
                )}
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="ring-destructive/30">
        <CardHeader>
          <CardTitle className="text-destructive">{msg('mcpTunnels.detail.dangerZone', 'Danger zone')}</CardTitle>
          <CardDescription>
            {msg(
              'mcpTunnels.detail.dangerDescription',
              'Archiving is permanent and immediately rejects connector and MCP requests.',
            )}
          </CardDescription>
          <CardAction>
            <Button
              type="button"
              variant="destructive"
              disabled={archived}
              onClick={() => setPendingAction({ type: 'archive', tunnel })}
            >
              <Archive className="size-4" aria-hidden /> {msg('common.archive', 'Archive')}
            </Button>
          </CardAction>
        </CardHeader>
      </Card>

      <Separator />
      <ProbeDialog probe={probeResult} onClose={() => setProbeResult(null)} />
      <ConfirmTunnelActionDialog
        action={pendingAction}
        busy={Boolean(pendingAction && busyAction === pendingAction.type)}
        onClose={() => setPendingAction(null)}
        onConfirm={() => void handleConfirmedAction()}
      />
    </section>
  );
}

function TunnelDetailMenu({
  tunnel,
  busy,
  onRotate,
  onArchive,
}: {
  tunnel: McpTunnel;
  busy: boolean;
  onRotate: () => void;
  onArchive: () => void;
}) {
  const { msg } = useI18n();
  const archived = Boolean(tunnel.archived_at);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label={msg('mcpTunnels.actionsFor', 'Actions for {name}', { name: tunnel.display_name || tunnel.id })}
            disabled={busy}
          />
        }
      >
        <MoreHorizontal className="size-4" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => void copyText(tunnel.id)}>
          <Copy /> {msg('mcpTunnels.action.copyId', 'Copy tunnel ID')}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void copyText(tunnel.mcp_url)}>
          <Copy /> {msg('mcpTunnels.action.copyMcpUrl', 'Copy MCP URL')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={archived} onClick={onRotate}>
          <RotateCw /> {msg('mcpTunnels.action.rotateToken', 'Rotate token')}
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" disabled={archived} onClick={onArchive}>
          <Archive /> {msg('common.archive', 'Archive')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function formatDate(value: string, formatter: (value: Date | number) => string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : formatter(date);
}

function readableError(error: unknown, fallback: string) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message || fallback;
  }
  return fallback;
}
