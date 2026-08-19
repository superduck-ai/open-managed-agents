import { Ban, BriefcaseBusiness, CheckCircle2, ChevronDown, Hand, Plus, Server, Trash2, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { cn } from '../../../shared/lib/utils';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Card } from '../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Input } from '../../../shared/ui/input';
import { Textarea } from '../../../shared/ui/textarea';
import { useI18n } from '../../../shared/i18n';
import { type CreateAgentInput } from '../types';
import { toRecord } from '../utils';
import {
  addBuiltInToolset,
  type EditablePermission,
  removeCustomTool,
  removeToolset,
  setToolPermission,
  setToolsetPermission,
  toolsetPermission,
  updateCustomTool,
} from './create-dialog-model';
import { CreateDialogMcpPicker, type CreateDialogMcpPickerProps } from './create-dialog-mcp-picker';
import {
  BUILT_IN_AGENT_TOOLSETS,
  builtInAgentToolDescription,
  effectiveToolPermission,
  type ToolPermissionState,
} from './tools/model';

const permissionValues: EditablePermission[] = ['always_allow', 'always_ask', 'always_deny'];

export function CreateDialogToolsEditor({
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
  const hasBuiltInToolset = draft.tools.some((tool) => tool.type === 'agent_toolset_20260401');

  return (
    <div className="space-y-3">
      {draft.tools.map((tool, index) => {
        if (tool.type === 'agent_toolset_20260401') {
          return (
            <ToolsetCard
              key="built-in-tools"
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
          const serverName = String(tool.mcp_server_name ?? '');
          const serverURL = configuredServerURL(draft, serverName);
          const server = directoryServers.find(
            (candidate) => candidate.slug === serverName && candidate.url === serverURL,
          );
          return (
            <ToolsetCard
              key={`mcp:${serverName}`}
              icon={<Server className="size-4" aria-hidden />}
              title={server?.displayName || serverName}
              subtitle={serverURL}
              toolset={tool}
              tools={(server?.toolNames ?? []).map((name) => ({ name }))}
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
        if (tool.type === 'custom') {
          return (
            <CustomToolCard
              key={`custom:${index}`}
              tool={tool}
              onChange={(update) => onChange(updateCustomTool(draft, index, update))}
              onRemove={() => onChange(removeCustomTool(draft, index))}
            />
          );
        }
        return null;
      })}

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
        <CreateDialogMcpPicker
          draft={draft}
          directoryServers={directoryServers}
          directoryLoading={directoryLoading}
          directoryError={directoryError}
          workspaceServers={workspaceServers}
          workspaceServersLoading={workspaceServersLoading}
          workspaceServersError={workspaceServersError}
          onRetryDirectory={onRetryDirectory}
          onRetryWorkspaceServers={onRetryWorkspaceServers}
          onCreateWorkspaceServer={onCreateWorkspaceServer}
          onChange={onChange}
        />
      </div>
    </div>
  );
}

function ToolsetCard({
  icon,
  title,
  subtitle,
  toolset,
  tools,
  fallback,
  onRemove,
  onGroupPermission,
  onToolPermission,
}: {
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  toolset: Record<string, unknown>;
  tools: Array<{ name: string; description?: string }>;
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
    <Card className="gap-0 overflow-hidden rounded-xl py-0 shadow-none">
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-9 items-center justify-center rounded-lg border border-border bg-background">
          {icon}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-medium">{title}</span>
          <code className="block truncate text-xs text-muted-foreground">{subtitle}</code>
        </span>
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
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex items-center gap-2 border-t border-border px-3 py-2">
          <CollapsibleTrigger
            type="button"
            className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 text-left text-sm text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            <ChevronDown className={cn('size-4 transition-transform', !open && '-rotate-90')} aria-hidden />
            <span>{msg('managedAgents.agents.createDialog.toolPermissions', 'Tool permissions')}</span>
            <Badge variant="secondary">{tools.length}</Badge>
          </CollapsibleTrigger>
          <PermissionMenu value={aggregate} onChange={onGroupPermission} />
        </div>
        <CollapsibleContent className="overflow-x-auto border-t border-border">
          {tools.length ? (
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
      <DropdownMenuContent align="end">
        <DropdownMenuRadioGroup value={value} onValueChange={(next) => onChange(next as EditablePermission)}>
          {permissionValues.map((permission) => (
            <DropdownMenuRadioItem key={permission} value={permission}>
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
  const buttons = [
    {
      value: 'always_allow' as const,
      label: msg('managedAgents.agents.detail.alwaysAllow', 'Always allow'),
      icon: CheckCircle2,
    },
    {
      value: 'always_ask' as const,
      label: msg('managedAgents.agents.detail.alwaysAsk', 'Always ask'),
      icon: Hand,
    },
    {
      value: 'always_deny' as const,
      label: msg('managedAgents.agents.detail.alwaysDeny', 'Always deny'),
      icon: Ban,
    },
  ];
  return (
    <div className="flex rounded-lg bg-muted p-0.5">
      {buttons.map((button) => {
        const Icon = button.icon;
        return (
          <Button
            key={button.value}
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={button.label}
            title={button.label}
            className={cn(
              'size-7 text-muted-foreground',
              value === button.value && 'bg-background text-foreground shadow-sm',
            )}
            onClick={() => onChange(button.value)}
          >
            <Icon className="size-4" aria-hidden />
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
  const serializedSchema = customToolSchemaText(tool.input_schema);
  const [schemaText, setSchemaText] = useState(serializedSchema);
  const publishedSchemaText = useRef(serializedSchema);
  useEffect(() => {
    if (serializedSchema === publishedSchemaText.current) {
      return;
    }
    publishedSchemaText.current = serializedSchema;
    setSchemaText(serializedSchema);
  }, [serializedSchema]);
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
            const nextText = event.target.value;
            setSchemaText(nextText);
            let inputSchema: unknown = nextText;
            try {
              inputSchema = JSON.parse(nextText) as unknown;
            } catch {
              inputSchema = nextText;
            }
            publishedSchemaText.current = customToolSchemaText(inputSchema);
            onChange({ input_schema: inputSchema });
          }}
        />
      </label>
    </Card>
  );
}

function customToolSchemaText(inputSchema: unknown) {
  return typeof inputSchema === 'string' ? inputSchema : (JSON.stringify(inputSchema ?? {}, null, 2) ?? '{}');
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
