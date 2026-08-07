import { useQuery } from '@tanstack/react-query';
import { Check, ChevronDown, ExternalLink, X } from 'lucide-react';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
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
import { listCreateAgentSkills, loadMcpDirectoryServers, searchCreateAgentSubagents } from './create-dialog-api';
import {
  type AgentModelOption,
  selectedSkillReferences,
  selectedSubagentReferences,
  toggleSkill,
  toggleSubagent,
  updateDraftModelID,
} from './create-dialog-model';
import { CreateDialogPicker } from './create-dialog-picker';
import { CreateDialogToolsEditor } from './create-dialog-tools-editor';

export function AgentConfigRenderedEditor({
  workspaceId,
  draft,
  modelOptions,
  onChange,
}: {
  workspaceId: string;
  draft: CreateAgentInput;
  modelOptions: AgentModelOption[];
  onChange: (next: CreateAgentInput) => void;
}) {
  const { msg } = useI18n();
  const [agentSearch, setAgentSearch] = useState('');
  const [debouncedAgentSearch, setDebouncedAgentSearch] = useState('');
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedAgentSearch(agentSearch), 300);
    return () => window.clearTimeout(timer);
  }, [agentSearch]);
  const agentsQuery = useQuery({
    queryKey: ['agent-config', 'subagents', workspaceId, debouncedAgentSearch.trim()],
    queryFn: () => searchCreateAgentSubagents(workspaceId, debouncedAgentSearch),
    retry: false,
  });
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
  const refetchAgents = agentsQuery.refetch;
  const refetchSkills = skillsQuery.refetch;

  useEffect(() => {
    const refresh = () => {
      void refetchAgents();
      void refetchSkills();
    };
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  }, [refetchAgents, refetchSkills]);

  const selectedSubagents = selectedSubagentReferences(draft);
  const selectedSubagentIds = selectedSubagents
    .filter((reference) => reference.type === 'agent')
    .map((reference) => reference.id);
  const selectedSkills = selectedSkillReferences(draft);
  const skillOptions = skillsQuery.data ?? [];
  const agents = agentsQuery.data ?? [];
  const agentOptions = agents.map((agent) => ({
    id: agent.id,
    label: agent.name,
    description: `${agent.id} · v${agent.version}`,
    disabled: selectedSubagents.length >= 20 && !selectedSubagentIds.includes(agent.id),
  }));
  const skillPickerOptions = skillOptions.map((skill) => ({
    id: skill.id,
    label: skill.displayTitle,
    description: `${skill.id} · ${skill.source}`,
    disabled: selectedSkills.length >= 20 && !selectedSkills.some((reference) => reference.skill_id === skill.id),
  }));

  return (
    <div className="space-y-0">
      <CreateDialogSection
        title={msg('managedAgents.agents.createDialog.general', 'General')}
        description={msg(
          'managedAgents.agents.createDialog.generalDescription',
          "What this agent is and how it's prompted.",
        )}
      >
        <div className="space-y-4">
          <label className="block space-y-1.5 text-sm font-medium">
            <span>{msg('managedAgents.common.name', 'Name')}</span>
            <Input
              className="h-10"
              value={draft.name}
              aria-invalid={!draft.name.trim()}
              onChange={(event) => onChange({ ...draft, name: event.target.value })}
            />
          </label>
          <label className="block space-y-1.5 text-sm font-medium">
            <span>{msg('managedAgents.common.model', 'Model')}</span>
            <ModelPicker
              options={modelOptions}
              value={typeof draft.model === 'string' ? draft.model : draft.model.id}
              onChange={(id) => onChange({ ...draft, model: updateDraftModelID(draft.model, id) })}
            />
          </label>
          <label className="block space-y-1.5 text-sm font-medium">
            <span>{msg('managedAgents.agents.createDialog.optionalDescription', 'Description (optional)')}</span>
            <Textarea
              className="min-h-20 resize-y"
              value={draft.description ?? ''}
              onChange={(event) => onChange({ ...draft, description: event.target.value || null })}
            />
          </label>
          <label className="block space-y-1.5 text-sm font-medium">
            <span>{msg('managedAgents.agents.createDialog.optionalSystemPrompt', 'System prompt (optional)')}</span>
            <Textarea
              className="min-h-28 resize-y"
              value={draft.system ?? ''}
              onChange={(event) => onChange({ ...draft, system: event.target.value || null })}
            />
          </label>
        </div>
      </CreateDialogSection>

      <Separator />
      <CreateDialogSection
        title={msg('managedAgents.agents.createDialog.multiagent', 'Multiagent')}
        description={msg(
          'managedAgents.agents.createDialog.multiagentDescription',
          'Subagents this agent can spawn and delegate work to.',
        )}
        learnMore="https://platform.claude.com/docs/en/managed-agents/multi-agent"
      >
        <SelectedPills
          items={selectedSubagents.map((reference) => {
            if (reference.type === 'self') {
              return { id: 'self', label: msg('managedAgents.agents.createDialog.thisAgent', 'This agent') };
            }
            const agent = agents.find((candidate) => candidate.id === reference.id);
            return { id: reference.id, label: agent?.name || reference.id };
          })}
          onRemove={(id) => {
            if (id === 'self') {
              const agentsWithoutSelf = selectedSubagents.filter((reference) => reference.type !== 'self');
              onChange({
                ...draft,
                multiagent: agentsWithoutSelf.length ? { type: 'coordinator', agents: agentsWithoutSelf } : null,
              });
              return;
            }
            const agent = agents.find((candidate) => candidate.id === id);
            if (agent) {
              onChange(toggleSubagent(draft, agent));
            } else {
              const remaining = selectedSubagents.filter(
                (reference) => reference.type !== 'agent' || reference.id !== id,
              );
              onChange({
                ...draft,
                multiagent: remaining.length ? { type: 'coordinator', agents: remaining } : null,
              });
            }
          }}
        />
        <CreateDialogPicker
          label={msg('managedAgents.agents.createDialog.addSubagent', 'Add subagent')}
          placeholder={msg('managedAgents.agents.createDialog.addSubagent', 'Add subagent')}
          searchPlaceholder={msg('managedAgents.agents.createDialog.searchAgents', 'Search agents...')}
          emptyLabel={msg('managedAgents.agents.createDialog.noAgents', 'No agents yet.')}
          options={agentOptions}
          selectedIds={selectedSubagentIds}
          loading={agentsQuery.isLoading}
          error={agentsQuery.isError}
          onRetry={() => void agentsQuery.refetch()}
          searchValue={agentSearch}
          onSearchChange={setAgentSearch}
          onToggle={(id) => {
            const agent = agents.find((candidate) => candidate.id === id);
            if (agent) {
              onChange(toggleSubagent(draft, agent));
            }
          }}
          createLabel={msg('managedAgents.agents.createLabel', 'Create agent')}
          onCreate={() => openInNewTab(agentCreateHref(workspaceId))}
        />
      </CreateDialogSection>

      <Separator />
      <CreateDialogSection
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
          emptyLabel={msg('managedAgents.agents.createDialog.noSkills', 'No skills yet.')}
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

      <Separator />
      <CreateDialogSection
        title={msg('managedAgents.agents.createDialog.tools', 'Tools')}
        description={msg(
          'managedAgents.agents.createDialog.toolsDescription',
          'Everything this agent can call: built-in tools, MCP servers, and custom tool definitions.',
        )}
        learnMore="https://platform.claude.com/docs/en/managed-agents/tools"
      >
        <CreateDialogToolsEditor
          draft={draft}
          directoryServers={directoryQuery.data ?? []}
          directoryLoading={directoryQuery.isLoading}
          directoryError={directoryQuery.isError}
          onRetryDirectory={() => void directoryQuery.refetch()}
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
  children,
}: {
  title: string;
  description: string;
  learnMore?: string;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  return (
    <section className="grid gap-5 py-8 md:grid-cols-[220px_minmax(0,1fr)] md:gap-6">
      <div>
        <h3 className="text-base font-semibold">{title}</h3>
        <p className="mt-2 max-w-64 text-sm leading-6 text-muted-foreground">{description}</p>
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
  onChange,
}: {
  options: AgentModelOption[];
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
            className="h-10 w-full justify-between font-mono font-normal"
          />
        }
      >
        <span className="truncate">{selected?.displayName || value}</span>
        <ChevronDown className="size-4 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(520px,calc(100vw-2rem))] gap-0 p-0">
        <Command>
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
                    <code className="block truncate text-xs text-muted-foreground">{option.id}</code>
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
            aria-label={`Remove ${item.label}`}
            onClick={() => onRemove(item.id)}
          >
            <X className="size-3" aria-hidden />
          </Button>
        </Badge>
      ))}
    </div>
  );
}

function agentCreateHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/agents?create_agent=1`;
}

function skillCreateHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/skills/new`;
}

function openInNewTab(path: string) {
  window.open(path, '_blank', 'noopener,noreferrer');
}
