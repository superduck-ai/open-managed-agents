import {
  Ban,
  BriefcaseBusiness,
  Cable,
  CheckCircle2,
  ChevronDown,
  Hand,
  LoaderCircle,
  Plus,
  RefreshCw,
  Server,
  Trash2,
  X,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { cn } from '../../../shared/lib/utils';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Card } from '../../../shared/ui/card';
import { Alert, AlertDescription, AlertTitle } from '../../../shared/ui/alert';
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '../../../shared/ui/combobox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { Textarea } from '../../../shared/ui/textarea';
import { useI18n } from '../../../shared/i18n';
import { toast } from '../../../shared/ui/sonner';
import { type CreateAgentInput, type I18nMsg } from '../types';
import { errorMessage, toRecord } from '../utils';
import {
  addBuiltInToolset,
  addCustomTool,
  addMcpServer,
  type EditablePermission,
  removeCustomTool,
  removeToolset,
  setToolPermission,
  setToolsetPermission,
  toolsetPermission,
  updateCustomTool,
  updateMcpServer,
} from './create-dialog-model';
import { CreateDialogPicker } from './create-dialog-picker';
import {
  BUILT_IN_AGENT_TOOLSETS,
  builtInAgentToolDescription,
  effectiveToolPermission,
  type McpDirectoryServer,
  type ToolPermissionState,
} from './tools/model';
import { RemoteServerIcon } from './tools/RemoteServerIcon';
import {
  DEFAULT_TUNNEL_CHANNEL,
  MCP_TUNNEL_CHANNEL_PATTERN,
  tunnelChannelFromURL,
  tunnelChannelServerName,
  tunnelChannelURL,
} from '../../mcp-tunnels/config';

const permissionOptions = [
  { value: 'always_allow' as const, icon: CheckCircle2 },
  { value: 'always_ask' as const, icon: Hand },
  { value: 'always_deny' as const, icon: Ban },
];

type PendingTunnelSelection = {
  server: McpDirectoryServer;
  channel: string;
};

export function CreateDialogToolsEditor({
  draft,
  directoryServers,
  directoryLoading,
  directoryError,
  onRetryDirectory,
  onProbeTunnel,
  onPendingTunnelChange,
  selectionResetKey = 0,
  onChange,
}: {
  draft: CreateAgentInput;
  directoryServers: McpDirectoryServer[];
  directoryLoading: boolean;
  directoryError: boolean;
  onRetryDirectory: () => void;
  onProbeTunnel: (tunnelId: string, channel: string) => Promise<Array<{ name: string; description?: string }>>;
  onPendingTunnelChange?: (pending: boolean) => void;
  selectionResetKey?: number;
  onChange: (next: CreateAgentInput) => void;
}) {
  const { msg } = useI18n();
  const [pendingTunnel, setPendingTunnel] = useState<PendingTunnelSelection | null>(null);
  const [focusedServerName, setFocusedServerName] = useState<string | null>(null);
  const [tunnelTools, setTunnelTools] = useState<Record<string, Array<{ name: string; description?: string }>>>({});
  const [tunnelToolsPending, setTunnelToolsPending] = useState<Set<string>>(() => new Set());
  const tunnelProbeRevisions = useRef<Record<string, number>>({});
  const configuredServerNames = draft.mcp_servers.flatMap((server) => {
    const name = toRecord(server)?.name;
    return typeof name === 'string' ? [name] : [];
  });
  const availableServers = directoryServers.filter((server) => {
    if (!server.url) return false;
    return server.source === 'tunnel' || !configuredServerNames.includes(server.slug);
  });
  const hasBuiltInToolset = draft.tools.some((tool) => tool.type === 'agent_toolset_20260401');

  useEffect(() => {
    setPendingTunnel(null);
    setFocusedServerName(null);
    setTunnelTools({});
    setTunnelToolsPending(new Set());
    tunnelProbeRevisions.current = {};
  }, [selectionResetKey]);

  useEffect(() => {
    onPendingTunnelChange?.(pendingTunnel !== null);
  }, [onPendingTunnelChange, pendingTunnel]);

  useEffect(
    () => () => {
      onPendingTunnelChange?.(false);
    },
    [onPendingTunnelChange],
  );

  return (
    <div className="space-y-3">
      {draft.tools.map((tool, index) => (
        <ConfiguredToolCard
          key={configuredToolKey(tool, index)}
          tool={tool}
          index={index}
          draft={draft}
          directoryServers={directoryServers}
          tunnelTools={tunnelTools}
          toolsLoading={tool.type === 'mcp_toolset' && tunnelToolsPending.has(String(tool.mcp_server_name ?? ''))}
          onRefreshTunnel={refreshTunnelTools}
          onUpdateTunnel={updateTunnelChannel}
          autoFocusChannel={tool.type === 'mcp_toolset' && tool.mcp_server_name === focusedServerName}
          onChange={onChange}
        />
      ))}

      {pendingTunnel ? (
        <PendingTunnelCard
          server={pendingTunnel.server}
          channel={pendingTunnel.channel}
          draft={draft}
          onChannelChange={(channel) => setPendingTunnel((current) => (current ? { ...current, channel } : null))}
          onCancel={() => setPendingTunnel(null)}
          onConfirm={(channel) => addTunnelChannel(pendingTunnel.server, channel)}
        />
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        {!hasBuiltInToolset ? (
          <Button
            type="button"
            variant="ghost"
            className="h-9 justify-start gap-2 px-2 text-sm font-normal text-foreground"
            aria-label={msg('managedAgents.agents.createDialog.addBuiltInTools', 'Add built-in tools')}
            onClick={() => onChange(addBuiltInToolset(draft))}
          >
            <Plus className="size-4" aria-hidden />
            {msg('managedAgents.agents.createDialog.addBuiltInTools', 'Add built-in tools')}
          </Button>
        ) : null}
        <CreateDialogPicker
          label={msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}
          placeholder={msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}
          searchPlaceholder={msg('managedAgents.agents.createDialog.searchMcpServers', 'Search MCP servers...')}
          emptyLabel={msg('managedAgents.agents.createDialog.noMcpServers', 'No MCP servers found.')}
          options={availableServers.map((server) => ({
            id: server.slug,
            label: server.displayName,
            description: server.source === 'tunnel' ? tunnelConnectionLabel(server, msg) : server.url,
            group: server.group,
            icon:
              server.source === 'tunnel' ? (
                <span className="flex size-8 items-center justify-center rounded-lg border border-border bg-background">
                  <Cable className="size-4" aria-hidden />
                </span>
              ) : (
                <RemoteServerIcon
                  iconUrl={server.iconUrl}
                  serverUrl={server.url}
                  className="size-8"
                  iconClassName="size-4"
                />
              ),
          }))}
          selectedIds={[]}
          loading={directoryLoading}
          error={directoryError}
          onRetry={onRetryDirectory}
          closeOnSelect
          onToggle={(id) => {
            const server = availableServers.find((candidate) => candidate.slug === id);
            if (server) {
              if (server.source === 'tunnel') {
                selectTunnel(server);
              } else {
                onChange(addMcpServer(draft, server));
              }
            }
          }}
        />
        <Button
          type="button"
          variant="ghost"
          className="h-9 justify-start gap-2 px-2 text-sm font-normal text-foreground"
          aria-label={msg('managedAgents.agents.createDialog.addCustomTool', 'Add custom tool')}
          onClick={() => onChange(addCustomTool(draft))}
        >
          <Plus className="size-4" aria-hidden />
          {msg('managedAgents.agents.createDialog.addCustomTool', 'Add custom tool')}
        </Button>
      </div>
    </div>
  );

  function selectTunnel(server: McpDirectoryServer) {
    const channels = liveTunnelChannels(server);
    if (channels.length === 1) {
      const resolved = configuredTunnelServer(server, channels[0]);
      if (resolved && !isTunnelChannelConfigured(draft, resolved.slug, resolved.url)) {
        addTunnelChannel(server, channels[0]);
        return;
      }
    }
    setPendingTunnel({ server, channel: '' });
    scrollToChannelCard(pendingTunnelCardID(server));
  }

  function addTunnelChannel(server: McpDirectoryServer, channel: string) {
    const resolved = configuredTunnelServer(server, channel);
    if (!resolved || isTunnelChannelConfigured(draft, resolved.slug, resolved.url)) return;
    onChange(addMcpServer(draft, resolved));
    setPendingTunnel(null);
    setFocusedServerName(resolved.slug);
    scrollToChannelCard(tunnelChannelCardID(resolved.slug));
    if (resolved.tunnel?.connectionState === 'connected') {
      void refreshTunnelTools(resolved.slug, resolved.tunnel.id, channel);
    }
  }

  function updateTunnelChannel(currentName: string, server: McpDirectoryServer, channel: string) {
    const resolved = configuredTunnelServer(server, channel);
    if (!resolved) return;
    const nextDraft = updateMcpServer(draft, currentName, resolved);
    if (nextDraft === draft) return;
    invalidateTunnelProbe(currentName);
    onChange(nextDraft);
    setTunnelTools((current) => withoutRecordKey(current, currentName));
    setTunnelToolsPending((current) => withoutSetValue(current, currentName));
    if (resolved.tunnel?.connectionState === 'connected') {
      void refreshTunnelTools(resolved.slug, resolved.tunnel.id, channel);
    }
  }

  async function refreshTunnelTools(serverName: string, tunnelId: string, channel: string) {
    const revision = invalidateTunnelProbe(serverName);
    setTunnelToolsPending((current) => new Set(current).add(serverName));
    try {
      const tools = await onProbeTunnel(tunnelId, channel);
      if (tunnelProbeRevisions.current[serverName] === revision) {
        setTunnelTools((current) => ({ ...current, [serverName]: tools }));
      }
    } catch (error) {
      if (tunnelProbeRevisions.current[serverName] === revision) {
        toast.error(msg('managedAgents.agents.detail.refreshMcpToolsFailed', 'Could not refresh MCP tools.'), {
          description: errorMessage(error),
        });
      }
    } finally {
      if (tunnelProbeRevisions.current[serverName] === revision) {
        setTunnelToolsPending((current) => withoutSetValue(current, serverName));
      }
    }
  }

  function invalidateTunnelProbe(serverName: string) {
    const revision = (tunnelProbeRevisions.current[serverName] ?? 0) + 1;
    tunnelProbeRevisions.current[serverName] = revision;
    return revision;
  }
}

function configuredToolKey(tool: CreateAgentInput['tools'][number], index: number) {
  if (tool.type === 'agent_toolset_20260401') return 'built-in-tools';
  if (tool.type === 'mcp_toolset') return `mcp:${String(tool.mcp_server_name ?? '')}`;
  return `custom:${index}`;
}

function ConfiguredToolCard({
  tool,
  index,
  draft,
  directoryServers,
  tunnelTools,
  toolsLoading,
  onRefreshTunnel,
  onUpdateTunnel,
  autoFocusChannel,
  onChange,
}: {
  tool: CreateAgentInput['tools'][number];
  index: number;
  draft: CreateAgentInput;
  directoryServers: McpDirectoryServer[];
  tunnelTools: Record<string, Array<{ name: string; description?: string }>>;
  toolsLoading: boolean;
  onRefreshTunnel: (serverName: string, tunnelId: string, channel: string) => Promise<void>;
  onUpdateTunnel: (serverName: string, server: McpDirectoryServer, channel: string) => void;
  autoFocusChannel: boolean;
  onChange: (next: CreateAgentInput) => void;
}) {
  const { msg } = useI18n();
  if (tool.type === 'agent_toolset_20260401') {
    return (
      <ToolsetCard
        icon={<BriefcaseBusiness className="size-4" aria-hidden />}
        title={msg('managedAgents.agents.createDialog.builtInTools', 'Built-in tools')}
        subtitle="agent_toolset_20260401"
        toolset={tool}
        tools={BUILT_IN_AGENT_TOOLSETS.agent_toolset_20260401}
        fallback="always_allow"
        onRemove={() => onChange(removeToolset(draft, 'agent_toolset_20260401'))}
        onGroupPermission={(permission) =>
          onChange(setToolsetPermission(draft, (candidate) => candidate === tool, permission))
        }
        onToolPermission={(name, permission) =>
          onChange(setToolPermission(draft, (candidate) => candidate === tool, name, permission, 'always_allow'))
        }
      />
    );
  }
  if (tool.type === 'mcp_toolset') {
    return (
      <ConfiguredMcpToolCard
        tool={tool}
        draft={draft}
        directoryServers={directoryServers}
        tunnelTools={tunnelTools}
        toolsLoading={toolsLoading}
        onRefreshTunnel={onRefreshTunnel}
        onUpdateTunnel={onUpdateTunnel}
        autoFocusChannel={autoFocusChannel}
        onChange={onChange}
      />
    );
  }
  if (tool.type === 'custom') {
    return (
      <CustomToolCard
        tool={tool}
        onChange={(update) => onChange(updateCustomTool(draft, index, update))}
        onRemove={() => onChange(removeCustomTool(draft, index))}
      />
    );
  }
  return null;
}

function ConfiguredMcpToolCard({
  tool,
  draft,
  directoryServers,
  tunnelTools,
  toolsLoading,
  onRefreshTunnel,
  onUpdateTunnel,
  autoFocusChannel,
  onChange,
}: {
  tool: Record<string, unknown>;
  draft: CreateAgentInput;
  directoryServers: McpDirectoryServer[];
  tunnelTools: Record<string, Array<{ name: string; description?: string }>>;
  toolsLoading: boolean;
  onRefreshTunnel: (serverName: string, tunnelId: string, channel: string) => Promise<void>;
  onUpdateTunnel: (serverName: string, server: McpDirectoryServer, channel: string) => void;
  autoFocusChannel: boolean;
  onChange: (next: CreateAgentInput) => void;
}) {
  const serverName = String(tool.mcp_server_name ?? '');
  const configuredURL = configuredServerURL(draft, serverName);
  const server = resolveConfiguredServer(directoryServers, configuredURL, serverName);
  const configuredTunnel = resolveConfiguredTunnel(server, configuredURL);
  const serverTitle = configuredMcpServerTitle(server, serverName, configuredTunnel?.channel);
  const tools = configuredMcpTools(server, serverName, configuredTunnel !== null, tunnelTools);
  return (
    <ToolsetCard
      cardID={configuredTunnel ? tunnelChannelCardID(serverName) : undefined}
      icon={configuredTunnel ? <Cable className="size-4" aria-hidden /> : <Server className="size-4" aria-hidden />}
      title={serverTitle}
      subtitle={configuredURL}
      toolset={tool}
      tools={tools}
      toolsLoading={toolsLoading}
      onRefreshTools={
        configuredTunnel
          ? () => void onRefreshTunnel(serverName, configuredTunnel.tunnelId, configuredTunnel.channel)
          : undefined
      }
      configuration={
        configuredTunnel ? (
          <ConfiguredTunnelChannelEditor
            configuredTunnel={configuredTunnel}
            serverName={serverName}
            draft={draft}
            autoFocus={autoFocusChannel}
            onUpdateTunnel={onUpdateTunnel}
          />
        ) : undefined
      }
      fallback="always_ask"
      onRemove={() => onChange(removeToolset(draft, serverName))}
      onGroupPermission={(permission) =>
        onChange(setToolsetPermission(draft, (candidate) => candidate === tool, permission))
      }
      onToolPermission={(name, permission) =>
        onChange(setToolPermission(draft, (candidate) => candidate === tool, name, permission, 'always_ask'))
      }
    />
  );
}

type ConfiguredTunnel = {
  server: McpDirectoryServer;
  tunnelId: string;
  channel: string;
};

function ConfiguredTunnelChannelEditor({
  configuredTunnel,
  serverName,
  draft,
  autoFocus,
  onUpdateTunnel,
}: {
  configuredTunnel: ConfiguredTunnel;
  serverName: string;
  draft: CreateAgentInput;
  autoFocus: boolean;
  onUpdateTunnel: (serverName: string, server: McpDirectoryServer, channel: string) => void;
}) {
  const { server, channel } = configuredTunnel;
  const [editedChannel, setEditedChannel] = useState(channel);
  useEffect(() => {
    setEditedChannel(channel);
  }, [channel]);
  return (
    <TunnelChannelEditor
      server={server}
      channel={editedChannel}
      currentServerName={serverName}
      currentChannel={channel}
      draft={draft}
      autoFocus={autoFocus}
      onChannelChange={setEditedChannel}
      onConfirm={(nextChannel) => onUpdateTunnel(serverName, server, nextChannel)}
      onCancel={() => setEditedChannel(channel)}
    />
  );
}

function PendingTunnelCard({
  server,
  channel,
  draft,
  onChannelChange,
  onCancel,
  onConfirm,
}: {
  server: McpDirectoryServer;
  channel: string;
  draft: CreateAgentInput;
  onChannelChange: (channel: string) => void;
  onCancel: () => void;
  onConfirm: (channel: string) => void;
}) {
  const { msg } = useI18n();
  if (!server.url || !server.tunnel) return null;
  return (
    <Card
      id={pendingTunnelCardID(server)}
      className="gap-0 overflow-hidden rounded-xl border-destructive/40 py-0 shadow-none ring-1 ring-destructive/10"
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-9 items-center justify-center rounded-lg border border-border bg-background">
          <Cable className="size-4" aria-hidden />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2 text-sm font-medium">
            {server.displayName}
            <Badge variant="destructive">
              {msg('managedAgents.agents.createDialog.channelRequired', 'Channel required')}
            </Badge>
          </span>
          <span className="block text-xs text-muted-foreground">
            {msg(
              'managedAgents.agents.createDialog.channelRequiredDescription',
              'Choose or enter the MCP channel before continuing.',
            )}
          </span>
        </span>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={msg('managedAgents.agents.createDialog.removeItem', 'Remove {name}', {
            name: server.displayName,
          })}
          onClick={onCancel}
        >
          <X className="size-4" aria-hidden />
        </Button>
      </div>
      <TunnelChannelEditor
        server={server}
        channel={channel}
        draft={draft}
        required
        autoFocus
        onChannelChange={onChannelChange}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />
    </Card>
  );
}

function TunnelChannelEditor({
  server,
  channel,
  currentServerName,
  currentChannel,
  draft,
  required = false,
  autoFocus = false,
  onChannelChange,
  onConfirm,
  onCancel,
}: {
  server: McpDirectoryServer;
  channel: string;
  currentServerName?: string;
  currentChannel?: string;
  draft: CreateAgentInput;
  required?: boolean;
  autoFocus?: boolean;
  onChannelChange: (channel: string) => void;
  onConfirm: (channel: string) => void;
  onCancel: () => void;
}) {
  const { msg } = useI18n();
  const baseURL = server.url;
  const tunnel = server.tunnel;
  if (!baseURL || !tunnel) return null;
  const suggestions = channelSuggestions(server);
  const state = tunnelChannelEditorState({
    draft,
    channel,
    currentChannel,
    currentServerName,
    tunnelId: tunnel.id,
    baseURL,
    required,
    msg,
  });
  const inputKey = currentServerName ?? tunnel.id;
  return (
    <div className="grid gap-4 border-t border-border bg-muted/20 px-4 py-4">
      <TunnelConnectionWarning connected={tunnel.connectionState === 'connected'} />
      <div className="grid content-start gap-2">
        <span className="flex flex-wrap items-center gap-2">
          <Label htmlFor={tunnelChannelInputID(inputKey)}>
            {msg('managedAgents.agents.createDialog.channel', 'Channel')}
          </Label>
          <Badge variant={tunnel.connectionState === 'connected' ? 'secondary' : 'outline'}>
            {tunnelStateLabel(server, msg)}
          </Badge>
          <ChannelRequiredBadge required={required} />
        </span>
        <Combobox
          items={suggestions}
          value={channel}
          inputValue={channel}
          onInputValueChange={(value) => onChannelChange(value)}
          onValueChange={(value) => {
            const selectedChannel = value ?? '';
            onChannelChange(selectedChannel);
            if (
              shouldCommitSuggestedChannel(
                draft,
                selectedChannel,
                currentChannel,
                currentServerName,
                tunnel.id,
                baseURL,
              )
            ) {
              onConfirm(selectedChannel);
            }
          }}
        >
          <ComboboxInput
            id={tunnelChannelInputID(inputKey)}
            placeholder={DEFAULT_TUNNEL_CHANNEL}
            aria-invalid={!state.valid || state.alreadyConfigured}
            aria-describedby={state.help || state.alreadyConfigured ? tunnelChannelHelpID(inputKey) : undefined}
            autoFocus={autoFocus}
          />
          <ComboboxContent>
            <ComboboxEmpty>
              {msg('managedAgents.agents.createDialog.noLiveChannels', 'No matching live channels')}
            </ComboboxEmpty>
            <ComboboxList>
              {(suggestion: string) => (
                <ComboboxItem key={suggestion} value={suggestion}>
                  <code>{suggestion}</code>
                </ComboboxItem>
              )}
            </ComboboxList>
          </ComboboxContent>
        </Combobox>
        {state.help || state.alreadyConfigured ? (
          <p
            id={tunnelChannelHelpID(inputKey)}
            className={cn('text-xs', state.hasError ? 'text-destructive' : 'text-muted-foreground')}
          >
            {state.alreadyConfigured
              ? msg(
                  'managedAgents.agents.createDialog.channelAlreadyConfigured',
                  'This tunnel channel is already configured for the agent.',
                )
              : state.help}
          </p>
        ) : null}
        <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
          <span className="shrink-0 font-medium">{msg('managedAgents.agents.createDialog.mcpUrl', 'MCP URL')}</span>
          <code className="min-w-0 truncate opacity-80" title={state.resolvedURL || undefined}>
            {state.resolvedURL || '—'}
          </code>
        </div>
      </div>
      <TunnelChannelActions
        channel={channel}
        required={required}
        state={state}
        onCancel={onCancel}
        onConfirm={onConfirm}
      />
    </div>
  );
}

type TunnelChannelEditorState = {
  valid: boolean;
  alreadyConfigured: boolean;
  dirty: boolean;
  hasError: boolean;
  resolvedURL: string;
  help: string | null;
};

function tunnelChannelEditorState({
  draft,
  channel,
  currentChannel,
  currentServerName,
  tunnelId,
  baseURL,
  required,
  msg,
}: {
  draft: CreateAgentInput;
  channel: string;
  currentChannel?: string;
  currentServerName?: string;
  tunnelId: string;
  baseURL: string;
  required: boolean;
  msg: I18nMsg;
}): TunnelChannelEditorState {
  const valid = MCP_TUNNEL_CHANNEL_PATTERN.test(channel);
  const resolvedURL = valid ? tunnelChannelURL({ mcp_url: baseURL }, channel) : '';
  const serverName = valid ? tunnelChannelServerName(tunnelId, channel) : '';
  const alreadyConfigured = valid
    ? isTunnelChannelConfigured(draft, serverName, resolvedURL, currentServerName)
    : false;
  const dirty = currentChannel === undefined || channel !== currentChannel;
  return {
    valid,
    alreadyConfigured,
    dirty,
    hasError: alreadyConfigured || (!valid && Boolean(channel)),
    resolvedURL,
    help: channelHelpMessage(channel, valid, required, msg),
  };
}

function shouldCommitSuggestedChannel(
  draft: CreateAgentInput,
  channel: string,
  currentChannel: string | undefined,
  currentServerName: string | undefined,
  tunnelId: string,
  baseURL: string,
) {
  if (!channel || channel === currentChannel || !MCP_TUNNEL_CHANNEL_PATTERN.test(channel)) return false;
  return !isTunnelChannelConfigured(
    draft,
    tunnelChannelServerName(tunnelId, channel),
    tunnelChannelURL({ mcp_url: baseURL }, channel),
    currentServerName,
  );
}

function TunnelConnectionWarning({ connected }: { connected: boolean }) {
  const { msg } = useI18n();
  if (connected) return null;
  return (
    <Alert>
      <Cable aria-hidden />
      <AlertTitle>
        {msg('managedAgents.agents.createDialog.tunnelNotReady', 'Tunnel is not currently connected')}
      </AlertTitle>
      <AlertDescription>
        {msg(
          'managedAgents.agents.createDialog.tunnelNotReadyDescription',
          'You can save this configuration now. It will become callable after tunnel-client connects and publishes the channel.',
        )}
      </AlertDescription>
    </Alert>
  );
}

function ChannelRequiredBadge({ required }: { required: boolean }) {
  const { msg } = useI18n();
  if (!required) return null;
  return (
    <Badge variant="destructive">{msg('managedAgents.agents.createDialog.channelRequired', 'Channel required')}</Badge>
  );
}

function TunnelChannelActions({
  channel,
  required,
  state,
  onCancel,
  onConfirm,
}: {
  channel: string;
  required: boolean;
  state: TunnelChannelEditorState;
  onCancel: () => void;
  onConfirm: (channel: string) => void;
}) {
  const { msg } = useI18n();
  if (!required && !state.dirty) return null;
  return (
    <div className="flex justify-end gap-2">
      <Button type="button" variant="outline" onClick={onCancel}>
        {required
          ? msg('common.cancel', 'Cancel')
          : msg('managedAgents.agents.createDialog.cancelChannelChange', 'Cancel changes')}
      </Button>
      <Button
        type="button"
        disabled={!state.valid || state.alreadyConfigured || !state.dirty}
        onClick={() => onConfirm(channel)}
      >
        {required
          ? msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')
          : msg('managedAgents.agents.createDialog.applyChannel', 'Apply channel')}
      </Button>
    </div>
  );
}

function resolveConfiguredTunnel(
  server: McpDirectoryServer | undefined,
  configuredURL: string,
): ConfiguredTunnel | null {
  if (server?.source !== 'tunnel' || !server.url || !server.tunnel) return null;
  const channel = tunnelChannelFromURL({ mcp_url: server.url }, configuredURL);
  return channel ? { server, tunnelId: server.tunnel.id, channel } : null;
}

function configuredMcpServerTitle(
  server: McpDirectoryServer | undefined,
  serverName: string,
  tunnelChannel: string | undefined,
) {
  const displayName = server?.displayName ?? serverName;
  return tunnelChannel ? `${displayName} · ${tunnelChannel}` : displayName;
}

function configuredMcpTools(
  server: McpDirectoryServer | undefined,
  serverName: string,
  isTunnel: boolean,
  tunnelTools: Record<string, Array<{ name: string; description?: string }>>,
) {
  const discovered = isTunnel ? tunnelTools[serverName] : undefined;
  return discovered ?? (server?.toolNames ?? []).map((name) => ({ name }));
}

function configuredTunnelServer(server: McpDirectoryServer, channel: string) {
  if (!server.url || !server.tunnel || !MCP_TUNNEL_CHANNEL_PATTERN.test(channel)) return null;
  return {
    ...server,
    slug: tunnelChannelServerName(server.tunnel.id, channel),
    url: tunnelChannelURL({ mcp_url: server.url }, channel),
  };
}

function isTunnelChannelConfigured(
  draft: CreateAgentInput,
  serverName: string,
  resolvedURL: string,
  currentServerName?: string,
) {
  return draft.mcp_servers.some((candidate) => {
    const record = toRecord(candidate);
    return record?.name !== currentServerName && (record?.name === serverName || record?.url === resolvedURL);
  });
}

function liveTunnelChannels(server: McpDirectoryServer) {
  return [...new Set(server.tunnel?.channels ?? [])];
}

function channelSuggestions(server: McpDirectoryServer) {
  const channels = liveTunnelChannels(server);
  return channels.length ? channels : [DEFAULT_TUNNEL_CHANNEL];
}

function channelHelpMessage(channel: string, valid: boolean, required: boolean, msg: I18nMsg) {
  if (required && !channel) {
    return msg(
      'managedAgents.agents.createDialog.channelRequiredDescription',
      'Choose or enter the MCP channel before continuing.',
    );
  }
  if (valid) return null;
  return msg(
    'managedAgents.agents.createDialog.channelInvalid',
    'Enter 1–64 lowercase letters, numbers, underscores, or hyphens.',
  );
}

function tunnelStateLabel(server: McpDirectoryServer, msg: I18nMsg) {
  const state = server.tunnel?.connectionState;
  if (state === 'connected') return msg('mcpTunnels.connection.connected', 'Connected');
  if (state === 'disconnected') return msg('mcpTunnels.connection.disconnected', 'Disconnected');
  return msg('mcpTunnels.connection.unknown', 'Unknown');
}

function withoutRecordKey<T>(record: Record<string, T>, key: string) {
  const next = { ...record };
  delete next[key];
  return next;
}

function withoutSetValue<T>(values: Set<T>, value: T) {
  const next = new Set(values);
  next.delete(value);
  return next;
}

function tunnelChannelCardID(serverName: string) {
  return `agent-tunnel-card-${serverName}`;
}

function pendingTunnelCardID(server: McpDirectoryServer) {
  return `agent-tunnel-card-pending-${server.tunnel?.id ?? server.slug}`;
}

function tunnelChannelInputID(serverName: string) {
  return `agent-tunnel-channel-${serverName}`;
}

function tunnelChannelHelpID(serverName: string) {
  return `${tunnelChannelInputID(serverName)}-help`;
}

function scrollToChannelCard(id: string) {
  window.requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView({ block: 'nearest' }));
}

function resolveConfiguredServer(servers: McpDirectoryServer[], configuredURL: string, serverName: string) {
  const exact = servers.find((candidate) => candidate.slug === serverName);
  if (exact) return exact;
  return servers.find(
    (candidate) =>
      candidate.source === 'tunnel' &&
      candidate.url &&
      tunnelChannelFromURL({ mcp_url: candidate.url }, configuredURL) !== null,
  );
}

function tunnelConnectionLabel(server: McpDirectoryServer, msg: I18nMsg) {
  const state = server.tunnel?.connectionState;
  const status =
    state === 'connected'
      ? msg('mcpTunnels.connection.connected', 'Connected')
      : state === 'disconnected'
        ? msg('mcpTunnels.connection.disconnected', 'Disconnected')
        : msg('mcpTunnels.connection.unknown', 'Unknown');
  return `${status} · ${server.url ?? ''}`;
}

function ToolsetCard({
  cardID,
  icon,
  title,
  subtitle,
  toolset,
  tools,
  toolsLoading = false,
  onRefreshTools,
  configuration,
  fallback,
  onRemove,
  onGroupPermission,
  onToolPermission,
}: {
  cardID?: string;
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  toolset: Record<string, unknown>;
  tools: Array<{ name: string; description?: string }>;
  toolsLoading?: boolean;
  onRefreshTools?: () => void;
  configuration?: React.ReactNode;
  fallback: EditablePermission;
  onRemove: () => void;
  onGroupPermission: (permission: EditablePermission) => void;
  onToolPermission: (name: string, permission: EditablePermission) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(true);
  const names = tools.map((tool) => tool.name);
  const aggregate = toolsetPermission(toolset, names, fallback);
  const defaultPermission = effectiveToolPermission(toRecord(toolset.default_config) ?? undefined, fallback);
  return (
    <Card id={cardID} className="gap-0 overflow-hidden rounded-xl py-0 shadow-none">
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-9 items-center justify-center rounded-lg border border-border bg-background">
          {icon}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-medium">{title}</span>
          <code className="block truncate text-xs text-muted-foreground">{subtitle}</code>
        </span>
        {onRefreshTools ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            disabled={toolsLoading}
            aria-label={msg('managedAgents.agents.detail.refreshMcpTools', 'Refresh MCP tools for {server}', {
              server: title,
            })}
            onClick={onRefreshTools}
          >
            <RefreshCw className={cn('size-4', toolsLoading && 'animate-spin')} aria-hidden />
          </Button>
        ) : null}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={msg('managedAgents.agents.createDialog.removeItem', 'Remove {name}', { name: title })}
          onClick={onRemove}
        >
          <X className="size-4" aria-hidden />
        </Button>
      </div>
      {configuration}
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex items-center gap-2 border-t border-border px-3 py-2">
          <CollapsibleTrigger
            type="button"
            className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 text-left text-sm text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            <ChevronDown className={cn('size-4 transition-transform', !open && '-rotate-90')} aria-hidden />
            <span>{msg('managedAgents.agents.createDialog.toolPermissions', 'Tool permissions')}</span>
            <Badge variant="secondary">
              {toolsLoading ? <LoaderCircle className="size-3 animate-spin" aria-hidden /> : tools.length}
            </Badge>
          </CollapsibleTrigger>
          <PermissionMenu value={aggregate} onChange={onGroupPermission} />
        </div>
        <CollapsibleContent className="overflow-x-auto border-t border-border">
          {toolsLoading ? (
            <p className="px-4 py-3 text-sm text-muted-foreground">
              {msg('managedAgents.agents.createDialog.discoveringMcpTools', 'Discovering MCP tools...')}
            </p>
          ) : tools.length ? (
            <div className="min-w-[560px]">
              {tools.map((tool) => {
                const override = Array.isArray(toolset.configs)
                  ? toolset.configs.map(toRecord).find((config) => config?.name === tool.name)
                  : undefined;
                const permission = effectiveToolPermission(override ?? undefined, defaultPermission);
                return (
                  <div
                    key={tool.name}
                    className="grid grid-cols-[150px_1fr_auto] items-center gap-4 border-b border-border/70 px-4 py-2 last:border-b-0"
                  >
                    <code className="text-xs">{tool.name}</code>
                    <span className="truncate text-xs text-muted-foreground">
                      {builtInAgentToolDescription({ name: tool.name, description: tool.description ?? '' }, msg)}
                    </span>
                    <PermissionButtons value={permission} onChange={(next) => onToolPermission(tool.name, next)} />
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="px-4 py-3 text-sm text-muted-foreground">
              {msg('managedAgents.agents.createDialog.toolNamesUnavailable', 'Tool names are unavailable.')}
            </p>
          )}
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
}

function PermissionMenu({
  value,
  onChange,
}: {
  value: ToolPermissionState;
  onChange: (permission: EditablePermission) => void;
}) {
  const { msg } = useI18n();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="min-w-28 justify-between"
            aria-label={msg('managedAgents.agents.createDialog.toolPermissions', 'Tool permissions')}
          />
        }
      >
        {permissionLabel(value, msg)}
        <ChevronDown className="size-3.5" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-max min-w-40">
        <DropdownMenuRadioGroup value={value} onValueChange={(next) => onChange(next as EditablePermission)}>
          {permissionOptions.map(({ value: permission, icon: PermissionIcon }) => (
            <DropdownMenuRadioItem key={permission} value={permission} className="whitespace-nowrap">
              <PermissionIcon data-slot="permission-option-icon" className="size-4 text-muted-foreground" aria-hidden />
              {permissionLabel(permission, msg)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function PermissionButtons({
  value,
  onChange,
}: {
  value: EditablePermission;
  onChange: (permission: EditablePermission) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex rounded-lg bg-muted p-0.5">
      {permissionOptions.map(({ value: permission, icon: PermissionIcon }) => {
        const label = permissionLabel(permission, msg);
        return (
          <Button
            key={permission}
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            title={label}
            className={cn(
              'size-7 text-muted-foreground',
              value === permission && 'bg-background text-foreground shadow-sm',
            )}
            onClick={() => onChange(permission)}
          >
            <PermissionIcon className="size-4" aria-hidden />
          </Button>
        );
      })}
    </div>
  );
}

function CustomToolCard({
  tool,
  onChange,
  onRemove,
}: {
  tool: Record<string, unknown>;
  onChange: (update: Record<string, unknown>) => void;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  const schemaText =
    typeof tool.input_schema === 'string' ? tool.input_schema : JSON.stringify(tool.input_schema ?? {}, null, 2);
  return (
    <Card className="gap-4 rounded-xl p-4 shadow-none">
      <div className="flex items-center gap-3">
        <span className="flex size-9 items-center justify-center rounded-lg border border-border bg-background">
          <Server className="size-4" aria-hidden />
        </span>
        <span className="flex-1 text-sm font-medium">
          {msg('managedAgents.agents.createDialog.customTool', 'Custom tool')}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={msg('managedAgents.agents.createDialog.removeItem', 'Remove {name}', {
            name: msg('managedAgents.agents.createDialog.customTool', 'Custom tool'),
          })}
          onClick={onRemove}
        >
          <Trash2 className="size-4" aria-hidden />
        </Button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="space-y-1.5 text-sm">
          <span>{msg('managedAgents.agents.createDialog.toolName', 'Name')}</span>
          <Input value={String(tool.name ?? '')} onChange={(event) => onChange({ name: event.target.value })} />
        </label>
        <label className="space-y-1.5 text-sm">
          <span>{msg('managedAgents.agents.createDialog.toolDescription', 'Description')}</span>
          <Input
            value={String(tool.description ?? '')}
            onChange={(event) => onChange({ description: event.target.value })}
          />
        </label>
      </div>
      <label className="space-y-1.5 text-sm">
        <span>{msg('managedAgents.agents.createDialog.inputSchema', 'Input schema')}</span>
        <Textarea
          className="min-h-32 font-mono text-xs"
          value={schemaText}
          onChange={(event) => {
            try {
              onChange({ input_schema: JSON.parse(event.target.value) as unknown });
            } catch {
              onChange({ input_schema: event.target.value });
            }
          }}
        />
      </label>
    </Card>
  );
}

function configuredServerURL(draft: CreateAgentInput, name: string) {
  const server = draft.mcp_servers.map(toRecord).find((candidate) => candidate?.name === name);
  return typeof server?.url === 'string' ? server.url : name;
}

function permissionLabel(permission: ToolPermissionState, msg: ReturnType<typeof useI18n>['msg']) {
  switch (permission) {
    case 'always_allow':
      return msg('managedAgents.agents.detail.alwaysAllow', 'Always allow');
    case 'always_ask':
      return msg('managedAgents.agents.detail.alwaysAsk', 'Always ask');
    case 'always_deny':
      return msg('managedAgents.agents.detail.alwaysDeny', 'Always deny');
    default:
      return msg('managedAgents.agents.detail.customPermission', 'Custom');
  }
}
