import { useQuery } from '@tanstack/react-query';
import { Check, ChevronDown, ExternalLink, X } from 'lucide-react';
import { useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { AuthContext } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { listWorkspaceMCPServers } from '../../../shared/api/workspaceMCPServers';
import { cn } from '../../../shared/lib/utils';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '../../../shared/ui/command';
import { Input } from '../../../shared/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { Separator } from '../../../shared/ui/separator';
import { Textarea } from '../../../shared/ui/textarea';
import { type CreateAgentInput } from '../types';
import { listMcpTunnels, probeMcpTunnel } from '../../mcp-tunnels/api';
import { listCreateAgentSkills, loadMcpDirectoryServers } from './create-dialog-api';
import { type AgentModelOption, selectedSkillReferences, toggleSkill, updateDraftModelID } from './create-dialog-model';
import { CreateDialogPicker } from './create-dialog-picker';
import { CreateDialogToolsEditor } from './create-dialog-tools-editor';
import { CreatePageSection } from './create-page-section';
import { SubagentsEditor } from './subagents-editor';
import { subagentSettings } from './subagents-model';

export function AgentConfigRenderedEditor({
  orgUuid,
  workspaceId,
  draft,
  modelOptions,
  onPendingTunnelChange,
  tunnelSelectionResetKey,
  compact,
  onChange,
}: {
  orgUuid?: string;
  workspaceId: string;
  draft: CreateAgentInput;
  modelOptions: AgentModelOption[];
  onPendingTunnelChange?: (pending: boolean) => void;
  tunnelSelectionResetKey?: number;
  compact?: boolean;
  onChange: (next: CreateAgentInput) => void;
}) {
  const { msg } = useI18n();
  const presentation = renderedEditorPresentation(compact === true);
  const csrfToken = useContext(AuthContext)?.csrfToken;
  const skillsQuery = useQuery({
    queryKey: ['skills', workspaceId, 'agent-config-picker'],
    queryFn: () => listCreateAgentSkills(workspaceId),
    retry: false,
  });
  const directoryQuery = useQuery({
    queryKey: ['managed-agents', 'mcp-directory', 'agent-config'],
    queryFn: loadMcpDirectoryServers,
    retry: false,
  });
  const workspaceServersQuery = useQuery({
    queryKey: ['workspace-mcp-servers', orgUuid ?? '', workspaceId, 'agent-picker'],
    queryFn: () => listAllWorkspaceMCPServers(orgUuid ?? '', workspaceId),
    enabled: Boolean(orgUuid && workspaceId),
    retry: false,
  });
  const tunnelsQuery = useQuery({
    queryKey: ['console-mcp-tunnels', orgUuid, workspaceId, false],
    queryFn: () => listMcpTunnels(orgUuid ?? '', workspaceId, false),
    enabled: Boolean(orgUuid && workspaceId),
    retry: false,
  });
  const refetchSkills = skillsQuery.refetch;
  const refetchWorkspaceServers = workspaceServersQuery.refetch;

  useEffect(() => {
    const refresh = () => {
      void refetchSkills();
      void refetchWorkspaceServers();
    };
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  }, [refetchSkills, refetchWorkspaceServers]);

  const selectedSkills = selectedSkillReferences(draft);
  const skillOptions = skillsQuery.data ?? [];
  const skillPickerOptions = skillOptions.map((skill) => ({
    id: skill.id,
    label: skill.displayTitle,
    description: `${skill.id}  ${skill.source}`,
    disabled: selectedSkills.length >= 20 && !selectedSkills.some((reference) => reference.skill_id === skill.id),
  }));
  const mcpServers = [
    ...(tunnelsQuery.data ?? []).map((tunnel) => ({
      slug: tunnel.id,
      displayName: tunnel.display_name || tunnel.id,
      url: tunnel.mcp_url,
      toolNames: [],
      source: 'tunnel' as const,
      group: msg('managedAgents.agents.createDialog.privateTunnels', 'Private tunnels'),
      tunnel: {
        id: tunnel.id,
        connectionState: tunnel.connection.state,
        channels: tunnel.connection.channels.map((channel) => channel.name),
      },
    })),
    ...(directoryQuery.data ?? []).map((server) => ({
      ...server,
      source: 'directory' as const,
      group: msg('managedAgents.agents.createDialog.mcpDirectory', 'MCP directory'),
    })),
  ];
  const mcpSourcesLoading = mcpServers.length === 0 && (directoryQuery.isLoading || tunnelsQuery.isLoading);
  const mcpSourcesUnavailable =
    mcpServers.length === 0 && !mcpSourcesLoading && (directoryQuery.isError || tunnelsQuery.isError);

  return (
    <div className={presentation.container}>
      <CreateDialogSection compact={compact} title={msg('managedAgents.agents.createDialog.general', 'General')}>
        <div className={cn('space-y-4', presentation.generalFields)}>
          <label className="grid gap-2 text-sm font-medium">
            <span>{msg('managedAgents.common.name', 'Name')}</span>
            <Input
              className="h-10"
              value={draft.name}
              aria-invalid={!draft.name.trim()}
              onChange={(event) => onChange({ ...draft, name: event.target.value })}
            />
          </label>
          <label className="grid gap-2 text-sm font-medium">
            <span>{msg('managedAgents.common.model', 'Model')}</span>
            <ModelPicker
              compact={compact === true}
              options={modelOptions}
              value={typeof draft.model === 'string' ? draft.model : draft.model.id}
              onChange={(id) => onChange({ ...draft, model: updateDraftModelID(draft.model, id) })}
            />
          </label>
          <label className="grid gap-2 text-sm font-medium">
            <span>{msg('managedAgents.agents.createDialog.optionalDescription', 'Description (optional)')}</span>
            <Textarea
              className={presentation.description}
              value={draft.description ?? ''}
              onChange={(event) => onChange({ ...draft, description: event.target.value || null })}
            />
          </label>
          <label className="grid gap-2 text-sm font-medium">
            <span>{msg('managedAgents.agents.createDialog.optionalSystemPrompt', 'System prompt (optional)')}</span>
            <Textarea
              className={presentation.system}
              value={draft.system ?? ''}
              onChange={(event) => onChange({ ...draft, system: event.target.value || null })}
            />
          </label>
        </div>
      </CreateDialogSection>

      <Separator className={presentation.separator} />
      <CreateDialogSection
        compact={compact}
        collapsible={compact}
        className={presentation.multiagentOrder}
        summary={
          subagentSettings(draft).type === 'enabled'
            ? msg('managedAgents.agents.createDialog.subagentsOn', 'Subagents on')
            : selectedConfigurationSummary(0, msg)
        }
        title={msg('managedAgents.agents.createDialog.multiagent', 'Multiagent')}
        description={msg(
          'managedAgents.agents.createDialog.multiagentDescription',
          'Subagents this agent can hand work to.',
        )}
        learnMore="https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration"
      >
        <SubagentsEditor draft={draft} workspaceId={workspaceId} orgUuid={orgUuid} onChange={onChange} />
      </CreateDialogSection>

      <Separator className={presentation.separator} />
      <CreateDialogSection
        compact={compact}
        collapsible={compact}
        className={presentation.skillsOrder}
        summary={selectedConfigurationSummary(selectedSkills.length, msg)}
        title={msg('managedAgents.agents.createDialog.skills', 'Skills')}
        description={msg(
          'managedAgents.agents.createDialog.skillsDescription',
          'Packaged instructions and scripts this agent loads when it needs them.',
        )}
        learnMore="https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview"
      >
        <SelectedPills
          items={selectedSkills.map((reference) => {
            const skill = skillOptions.find((candidate) => candidate.id === reference.skill_id);
            return { id: reference.skill_id, label: skill?.displayTitle || reference.skill_id };
          })}
          onRemove={(id) => {
            const skill = skillOptions.find((candidate) => candidate.id === id);
            if (skill) {
              onChange(toggleSkill(draft, skill));
            } else {
              onChange({ ...draft, skills: selectedSkills.filter((reference) => reference.skill_id !== id) });
            }
          }}
        />
        <CreateDialogPicker
          label={msg('managedAgents.agents.createDialog.addSkill', 'Add skill')}
          placeholder={msg('managedAgents.agents.createDialog.addSkill', 'Add skill')}
          searchPlaceholder={msg('managedAgents.agents.createDialog.searchSkills', 'Search skills...')}
          emptyLabel={msg('managedAgents.agents.createDialog.noSkills', 'No skills found.')}
          options={skillPickerOptions}
          selectedIds={selectedSkills.map((reference) => reference.skill_id)}
          loading={skillsQuery.isLoading}
          error={skillsQuery.isError}
          onRetry={() => void skillsQuery.refetch()}
          onToggle={(id) => {
            const skill = skillOptions.find((candidate) => candidate.id === id);
            if (skill) {
              onChange(toggleSkill(draft, skill));
            }
          }}
          createLabel={msg('skills.create', 'Create skill')}
          onCreate={() => openInNewTab(skillCreateHref(workspaceId))}
        />
      </CreateDialogSection>

      <Separator className={presentation.separator} />
      <CreateDialogSection
        compact={compact}
        collapsible={compact}
        className={presentation.toolsOrder}
        summary={toolConfigurationSummary(draft, msg)}
        title={msg('managedAgents.agents.createDialog.tools', 'Tools')}
        description={msg(
          'managedAgents.agents.createDialog.toolsDescription',
          'Everything this agent can call: built-in tools, MCP servers, and existing custom tools.',
        )}
        learnMore="https://platform.claude.com/docs/en/managed-agents/tools"
      >
        <CreateDialogToolsEditor
          draft={draft}
          directoryServers={mcpServers}
          directoryLoading={mcpSourcesLoading}
          directoryError={mcpSourcesUnavailable}
          workspaceServers={workspaceServersQuery.data?.data ?? []}
          workspaceServersLoading={workspaceServersQuery.isLoading}
          workspaceServersError={workspaceServersQuery.isError}
          onRetryWorkspaceServers={() => void workspaceServersQuery.refetch()}
          onCreateWorkspaceServer={() => openInNewTab(mcpServerCreateHref(workspaceId))}
          onRetryDirectory={() => {
            void directoryQuery.refetch();
            void tunnelsQuery.refetch();
          }}
          onProbeTunnel={async (tunnelId, channel) => {
            const result = await probeMcpTunnel(orgUuid ?? '', workspaceId, tunnelId, channel, csrfToken);
            return result.tools.map((tool) => ({ name: tool.name, description: tool.description }));
          }}
          onPendingTunnelChange={onPendingTunnelChange}
          selectionResetKey={tunnelSelectionResetKey}
          onChange={onChange}
        />
      </CreateDialogSection>
    </div>
  );
}

function CreateDialogSection({
  title,
  description,
  learnMore,
  compact = false,
  collapsible = false,
  summary,
  className,
  children,
}: {
  title: string;
  description?: string;
  learnMore?: string;
  compact?: boolean;
  collapsible?: boolean;
  summary?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  if (collapsible)
    return (
      <CreatePageSection
        title={title}
        description={description}
        learnMore={learnMore}
        summary={summary}
        className={className}
      >
        {children}
      </CreatePageSection>
    );
  return (
    <section
      className={cn(
        'grid gap-5 md:gap-6',
        compact
          ? 'border-b-[0.5px] border-border py-4 md:grid-cols-[244px_minmax(0,1fr)]'
          : 'py-8 md:grid-cols-[220px_minmax(0,1fr)]',
        className,
      )}
    >
      <div>
        <h3 className={compact ? 'text-sm font-medium' : 'text-sm font-semibold'}>{title}</h3>
        {description ? <p className="mt-2 max-w-64 text-sm leading-6 text-muted-foreground">{description}</p> : null}
        {learnMore ? (
          <a
            href={learnMore}
            target="_blank"
            rel="noreferrer"
            className="mt-1 inline-flex items-center gap-1 text-sm text-primary underline-offset-4 hover:underline"
          >
            {msg('common.learnMore', 'Learn more')}
            <ExternalLink className="size-3.5" aria-hidden />
          </a>
        ) : null}
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
}

function ModelPicker({
  options,
  value,
  compact,
  onChange,
}: {
  options: AgentModelOption[];
  compact: boolean;
  value: string;
  onChange: (id: string) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const normalizedOptions = useMemo(
    () => (options.some((option) => option.id === value) ? options : [{ id: value, displayName: value }, ...options]),
    [options, value],
  );
  const selected = normalizedOptions.find((option) => option.id === value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            role="combobox"
            aria-label={msg('managedAgents.common.model', 'Model')}
            aria-expanded={open}
            className={cn(
              'w-full justify-between font-normal',
              compact ? 'h-9 max-w-[344px] bg-secondary font-sans' : 'h-10 font-mono',
            )}
          />
        }
      >
        <span className="truncate">{selected?.displayName || value}</span>
        <ChevronDown className="size-4 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(520px,calc(100vw-2rem))] gap-0 p-0">
        <Command
          defaultValue={selected ? `${selected.displayName} ${selected.id}` : undefined}
          label={msg('managedAgents.agents.createDialog.searchModels', 'Search models...')}
        >
          <CommandInput placeholder={msg('managedAgents.agents.createDialog.searchModels', 'Search models...')} />
          <CommandList>
            <CommandEmpty>{msg('managedAgents.agents.createDialog.noModels', 'No models found.')}</CommandEmpty>
            <CommandGroup>
              {normalizedOptions.map((option) => (
                <CommandItem
                  key={option.id}
                  value={`${option.displayName} ${option.id}`}
                  onSelect={() => {
                    onChange(option.id);
                    setOpen(false);
                  }}
                >
                  <Check className={cn('size-4', value === option.id ? 'opacity-100' : 'opacity-0')} aria-hidden />
                  <span className="min-w-0">
                    <span className="block truncate">{option.displayName}</span>
                    {option.displayName !== option.id ? (
                      <code className="block truncate text-xs text-muted-foreground">{option.id}</code>
                    ) : null}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function SelectedPills({
  items,
  onRemove,
}: {
  items: Array<{ id: string; label: string }>;
  onRemove: (id: string) => void;
}) {
  const { msg } = useI18n();
  if (!items.length) {
    return null;
  }
  return (
    <div className="mb-2 flex flex-wrap gap-2">
      {items.map((item) => (
        <Badge key={item.id} variant="secondary" className="gap-1.5 py-1 pl-2.5 pr-1">
          <span className="max-w-64 truncate">{item.label}</span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="size-5 rounded-full"
            aria-label={msg('managedAgents.agents.createDialog.removeItem', 'Remove {name}', { name: item.label })}
            onClick={() => onRemove(item.id)}
          >
            <X className="size-3" aria-hidden />
          </Button>
        </Badge>
      ))}
    </div>
  );
}

function skillCreateHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/skills/new`;
}

function mcpServerCreateHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/mcp-servers/new`;
}

async function listAllWorkspaceMCPServers(orgUuid: string, workspaceId: string) {
  const data = [];
  let page: string | undefined;
  const seenPages = new Set<string>();
  while (true) {
    const response = await listWorkspaceMCPServers(orgUuid, workspaceId, { page });
    data.push(...response.data);
    const nextPage = response.next_page || undefined;
    if (!nextPage) {
      return { data, next_page: null };
    }
    if (seenPages.has(nextPage)) {
      throw new Error('MCP Servers pagination returned a repeated cursor.');
    }
    seenPages.add(nextPage);
    page = nextPage;
  }
}

function openInNewTab(path: string) {
  window.open(path, '_blank', 'noopener,noreferrer');
}

function renderedEditorPresentation(compact: boolean) {
  return compact
    ? {
        container: 'flex flex-col',
        generalFields: '[&_input]:h-9 [&_input]:bg-secondary [&_textarea]:bg-secondary',
        description: 'min-h-[60px] resize-y',
        system: 'min-h-20 resize-y',
        separator: 'hidden',
        toolsOrder: 'order-1',
        skillsOrder: 'order-2',
        multiagentOrder: 'order-3',
      }
    : {
        container: 'space-y-0',
        generalFields: '',
        description: 'min-h-20 resize-y',
        system: 'min-h-28 resize-y',
        separator: undefined,
        toolsOrder: undefined,
        skillsOrder: undefined,
        multiagentOrder: undefined,
      };
}

function selectedConfigurationSummary(count: number, msg: ReturnType<typeof useI18n>['msg']) {
  return count ? String(count) : msg('common.none', 'None');
}

function toolConfigurationSummary(draft: CreateAgentInput, msg: ReturnType<typeof useI18n>['msg']) {
  if (draft.tools.length === 1 && draft.tools[0].type === 'agent_toolset_20260401')
    return msg('managedAgents.agents.detail.builtInTools', 'Built-in tools');
  return draft.tools.length
    ? msg('managedAgents.agents.createDialog.configuredTools', '{count} toolsets', { count: draft.tools.length })
    : msg('common.none', 'None');
}
