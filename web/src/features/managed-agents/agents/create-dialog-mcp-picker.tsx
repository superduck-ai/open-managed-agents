import { ChevronsUpDown, ExternalLink, Plus } from 'lucide-react';
import { useCallback, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { type WorkspaceMCPServer } from '../../../shared/api/workspaceMCPServers';
import { Button } from '../../../shared/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { type CreateAgentInput } from '../types';
import { toRecord } from '../utils';
import { addMcpServer } from './create-dialog-model';
import { CreateDialogPickerList } from './create-dialog-picker';
import { type McpDirectoryServer } from './tools/model';
import { RemoteServerIcon } from './tools/RemoteServerIcon';

type McpPickerTab = 'directory' | 'custom';

export type CreateDialogMcpPickerProps = {
  draft: CreateAgentInput;
  directoryServers: McpDirectoryServer[];
  directoryLoading: boolean;
  directoryError: boolean;
  workspaceServers: WorkspaceMCPServer[];
  workspaceServersLoading: boolean;
  workspaceServersError: boolean;
  onRetryDirectory: () => void;
  onRetryWorkspaceServers: () => void;
  onCreateWorkspaceServer: () => void;
  onChange: (next: CreateAgentInput) => void;
};

export function CreateDialogMcpPicker({
  draft,
  directoryServers,
  directoryLoading,
  directoryError,
  workspaceServers,
  workspaceServersLoading,
  workspaceServersError,
  onRetryDirectory,
  onRetryWorkspaceServers,
  onCreateWorkspaceServer,
  onChange,
}: CreateDialogMcpPickerProps) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [activeTab, setActiveTab] = useState<McpPickerTab>('directory');
  const [searchValue, setSearchValue] = useState('');
  const [addFailed, setAddFailed] = useState(false);
  const atLimit = draft.mcp_servers.length >= 20;
  const configuredServerNames = draft.mcp_servers.flatMap((server) => {
    const serverName = toRecord(server)?.name;
    return typeof serverName === 'string' ? [serverName] : [];
  });
  const availableServers = directoryServers.filter(
    (server) => server.url && !configuredServerNames.includes(server.slug),
  );
  const availableWorkspaceServers = workspaceServers.filter(
    (server) => server.status === 'active' && !configuredServerNames.includes(server.name),
  );
  const conflictingWorkspaceServers = workspaceServers.filter((server) => {
    if (server.status !== 'active') {
      return false;
    }
    const configured = draft.mcp_servers.find((candidate) => toRecord(candidate)?.name === server.name);
    const configuredURL = toRecord(configured)?.url;
    return typeof configuredURL === 'string' && configuredURL !== server.url;
  });
  const normalizedSearch = searchValue.trim().toLowerCase();
  const filteredServers = normalizedSearch
    ? availableServers.filter((server) =>
        `${server.displayName} ${server.slug} ${server.url}`.toLowerCase().includes(normalizedSearch),
      )
    : availableServers;
  const filteredWorkspaceServers = normalizedSearch
    ? availableWorkspaceServers.filter((server) =>
        `${server.name} ${server.url}`.toLowerCase().includes(normalizedSearch),
      )
    : availableWorkspaceServers;

  const reset = useCallback(() => {
    setActiveTab('directory');
    setSearchValue('');
    setAddFailed(false);
  }, []);

  const close = useCallback(() => {
    setOpen(false);
    reset();
  }, [reset]);

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    if (!nextOpen) {
      reset();
    }
  };

  const addDirectoryServer = (id: string) => {
    const server = availableServers.find((candidate) => candidate.slug === id);
    if (!server?.url) {
      return;
    }
    const result = addMcpServer(draft, { name: server.slug, url: server.url });
    if (!result.ok) {
      setAddFailed(true);
      return;
    }
    onChange(result.draft);
    close();
  };

  const addWorkspaceServer = (id: string) => {
    const server = availableWorkspaceServers.find((candidate) => candidate.id === id);
    if (!server) {
      return;
    }
    const result = addMcpServer(draft, { name: server.name, url: server.url });
    if (!result.ok) {
      setAddFailed(true);
      return;
    }
    onChange(result.draft);
    close();
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            role="combobox"
            aria-expanded={open}
            aria-label={msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}
            className="h-9 justify-start gap-2 px-2 text-sm font-normal text-foreground"
          />
        }
      >
        <Plus className="size-4" aria-hidden />
        {msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}
        <ChevronsUpDown className="ml-auto size-3.5 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(420px,calc(100vw-2rem))] gap-0 p-0">
        <Tabs value={activeTab} className="gap-0" onValueChange={(value) => setActiveTab(value as McpPickerTab)}>
          <div className="border-b border-border p-2">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="directory">
                {msg('managedAgents.agents.createDialog.mcpDirectoryTab', 'Directory')}
              </TabsTrigger>
              <TabsTrigger value="custom">
                {msg('managedAgents.agents.createDialog.customMcpTab', 'Custom MCP')}
              </TabsTrigger>
            </TabsList>
          </div>
          <TabsContent value="directory" className="mt-0">
            <CreateDialogPickerList
              searchPlaceholder={msg('managedAgents.agents.createDialog.searchMcpServers', 'Search MCP servers...')}
              emptyLabel={msg('managedAgents.agents.createDialog.noMcpServers', 'No MCP servers found.')}
              options={filteredServers.map((server) => ({
                id: server.slug,
                label: server.displayName,
                description: server.url,
                disabled: atLimit,
                icon: <RemoteServerIcon directoryIconUrl={server.iconUrl} className="size-8" iconClassName="size-4" />,
              }))}
              selectedIds={[]}
              loading={directoryLoading}
              error={directoryError}
              onRetry={onRetryDirectory}
              onToggle={addDirectoryServer}
              searchValue={searchValue}
              onSearchChange={setSearchValue}
            />
            <MCPPickerAlerts atLimit={atLimit} addFailed={addFailed} />
          </TabsContent>
          <TabsContent value="custom" className="mt-0">
            <CreateDialogPickerList
              searchPlaceholder={msg(
                'managedAgents.agents.createDialog.searchCustomMcpServers',
                'Search custom MCP servers...',
              )}
              emptyLabel={msg('managedAgents.agents.createDialog.noCustomMcpServers', 'No custom MCP servers yet.')}
              options={filteredWorkspaceServers.map((server) => ({
                id: server.id,
                label: server.name,
                description: server.url,
                disabled: atLimit,
                icon: <RemoteServerIcon className="size-8" iconClassName="size-4" />,
              }))}
              selectedIds={[]}
              loading={workspaceServersLoading}
              error={workspaceServersError}
              onRetry={onRetryWorkspaceServers}
              onToggle={addWorkspaceServer}
              searchValue={searchValue}
              onSearchChange={setSearchValue}
            />
            <MCPPickerAlerts atLimit={atLimit} addFailed={addFailed} />
            {conflictingWorkspaceServers.length > 0 ? (
              <p role="alert" className="border-t border-border px-3 py-2 text-sm text-destructive">
                {msg(
                  'managedAgents.agents.createDialog.mcpNameUrlConflict',
                  'This Agent already uses {names} with a different URL. Remove the existing MCP before adding the workspace version.',
                  { names: conflictingWorkspaceServers.map((server) => server.name).join(', ') },
                )}
              </p>
            ) : null}
            <div className="border-t border-border p-1.5">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="w-full justify-start"
                onClick={() => {
                  onCreateWorkspaceServer();
                  close();
                }}
              >
                <ExternalLink className="size-4" aria-hidden />
                {msg('mcpServers.create', 'Create MCP server')}
              </Button>
            </div>
          </TabsContent>
        </Tabs>
      </PopoverContent>
    </Popover>
  );
}

function MCPPickerAlerts({ atLimit, addFailed }: { atLimit: boolean; addFailed: boolean }) {
  const { msg } = useI18n();
  return (
    <>
      {atLimit ? (
        <p role="alert" className="border-t border-border px-3 py-2 text-sm text-destructive">
          {msg('managedAgents.agents.createDialog.mcpServerLimit', 'An agent can use at most 20 MCP servers.')}
        </p>
      ) : null}
      {addFailed ? (
        <p role="alert" className="border-t border-border px-3 py-2 text-sm text-destructive">
          {msg('managedAgents.agents.createDialog.mcpAddFailed', 'Could not add this MCP server.')}
        </p>
      ) : null}
    </>
  );
}
