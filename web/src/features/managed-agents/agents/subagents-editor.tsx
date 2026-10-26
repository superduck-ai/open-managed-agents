import { useQuery } from '@tanstack/react-query';
import { useEffect, useId, useRef, useState } from 'react';
import { Bot, X } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Checkbox } from '@/shared/ui/checkbox';
import { Button } from '@/shared/ui/button';
import { Badge } from '@/shared/ui/badge';
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/shared/ui/field';
import type { AgentApiResponse, AgentDelegationInput, CreateAgentInput, SubagentReference } from '../types';
import { retrieveAgent } from '../api';
import { AgentQuickView } from './AgentQuickView';
import { searchCreateAgentSubagents } from './create-dialog-api';
import { CreateDialogPicker } from './create-dialog-picker';
import { selectedSubagentReferences, subagentSettings, toggleSubagent, updateSubagents } from './subagents-model';

export function SubagentsEditor({
  draft,
  workspaceId,
  orgUuid,
  onChange,
}: {
  draft: CreateAgentInput;
  workspaceId: string;
  orgUuid?: string;
  onChange: (draft: CreateAgentInput) => void;
}) {
  const { msg } = useI18n();
  const settings = subagentSettings(draft);
  const enabled = settings.type === 'enabled';
  const inline = enabled && settings.inline_agents?.type !== 'disabled';
  const retained = useRef<{ previous: AgentDelegationInput; disabled: CreateAgentInput['multiagent'] } | undefined>(
    undefined,
  );
  const selected = selectedSubagentReferences(draft);
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(search), 300);
    return () => window.clearTimeout(timer);
  }, [search]);
  const agents = useQuery({
    queryKey: ['agent-config', 'subagents', workspaceId, query.trim()],
    queryFn: () => searchCreateAgentSubagents(workspaceId, query),
    enabled,
    retry: false,
  });
  const commit = (next: AgentDelegationInput) => onChange(updateSubagents(draft, next));
  return (
    <div className="space-y-4">
      <SubagentCheckbox
        checked={enabled}
        label={msg('managedAgents.agents.createDialog.subagents', 'Subagents')}
        description={msg(
          'managedAgents.agents.createDialog.subagentsDescription',
          'Hands tasks to other agents, each in a thread of its own.',
        )}
        onChange={(checked) => {
          if (checked)
            commit(
              (retained.current?.disabled === draft.multiagent ? retained.current?.previous : undefined) ?? {
                type: 'enabled',
                inline_agents: { type: 'enabled' },
                predefined_agents: [],
              },
            );
          else {
            const next = updateSubagents(draft, { type: 'disabled' });
            retained.current = { previous: settings, disabled: next.multiagent };
            onChange(next);
          }
        }}
      />
      {enabled ? (
        <div className="space-y-4 pl-6">
          <SubagentCheckbox
            checked={inline}
            label={msg('managedAgents.agents.createDialog.inlineAgents', 'Can define its own agents')}
            description={msg(
              'managedAgents.agents.createDialog.inlineAgentsDescription',
              'Agents it writes for one task. They aren’t saved.',
            )}
            onChange={(checked) => commit({ ...settings, inline_agents: { type: checked ? 'enabled' : 'disabled' } })}
          />
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">
              {msg('managedAgents.agents.createDialog.savedAgents', 'Saved agents it can use')}
            </p>
            <div className="flex flex-wrap items-center gap-2">
              {selected.map((reference) => (
                <SubagentChip
                  key={reference.type === 'self' ? 'self' : reference.id}
                  reference={reference}
                  initialAgent={agents.data?.find(
                    (agent) =>
                      reference.type === 'agent' && agent.id === reference.id && agent.version === reference.version,
                  )}
                  workspaceId={workspaceId}
                  orgUuid={orgUuid}
                  onRemove={() =>
                    commit({ ...settings, predefined_agents: selected.filter((entry) => entry !== reference) })
                  }
                />
              ))}
              {selected.length < 20 ? (
                <CreateDialogPicker
                  label={msg('managedAgents.agents.createDialog.addAgent', 'Add agent')}
                  placeholder={msg('managedAgents.agents.createDialog.addAgent', 'Add agent')}
                  searchPlaceholder={msg(
                    'managedAgents.agents.createDialog.searchAgents',
                    'Search agents by name or exact ID',
                  )}
                  emptyLabel={msg('managedAgents.agents.createDialog.noAgents', 'No agents yet.')}
                  options={(agents.data ?? [])
                    .filter((agent) => !selected.some((entry) => entry.type === 'agent' && entry.id === agent.id))
                    .map((agent) => ({
                      id: agent.id,
                      label: agent.name,
                      description: `${agent.id} · v${agent.version}`,
                    }))}
                  selectedIds={[]}
                  searchValue={search}
                  onSearchChange={setSearch}
                  loading={agents.isLoading}
                  error={agents.isError}
                  onRetry={() => void agents.refetch()}
                  closeOnSelect
                  onToggle={(id) => {
                    const agent = agents.data?.find((entry) => entry.id === id);
                    if (agent) onChange(toggleSubagent(draft, agent));
                  }}
                />
              ) : null}
            </div>
            {!inline && !selected.length ? (
              <p role="alert" className="text-sm text-destructive">
                {msg(
                  'managedAgents.agents.createDialog.emptySubagents',
                  'Add a saved agent, or let it define its own.',
                )}
              </p>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function SubagentCheckbox({
  label,
  description,
  checked,
  onChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const id = useId();
  return (
    <Field orientation="horizontal">
      <Checkbox id={id} checked={checked} onCheckedChange={onChange} aria-describedby={`${id}-description`} />
      <FieldContent>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        <FieldDescription id={`${id}-description`}>{description}</FieldDescription>
      </FieldContent>
    </Field>
  );
}

function SubagentChip({
  reference,
  initialAgent,
  workspaceId,
  orgUuid,
  onRemove,
}: {
  reference: SubagentReference;
  initialAgent?: AgentApiResponse;
  workspaceId: string;
  orgUuid?: string;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const id = reference.type === 'agent' ? reference.id : '';
  const version = reference.type === 'agent' ? reference.version : undefined;
  const query = useQuery({
    queryKey: ['subagent-reference', workspaceId, id, version],
    initialData: initialAgent,
    staleTime: 30_000,
    queryFn: () => retrieveAgent(id, workspaceId, version),
    enabled: Boolean(id),
    retry: false,
  });
  const label = id ? (query.data?.name ?? id) : msg('managedAgents.agents.createDialog.thisAgent', 'This agent');
  return (
    <>
      <Badge variant="secondary" className="max-w-full gap-1 py-0.5 pr-1">
        <Button
          variant="ghost"
          size="sm"
          className="h-6 min-w-0 gap-1 px-1 font-normal"
          disabled={!id}
          onClick={() => setOpen(true)}
        >
          <Bot className="size-3.5 shrink-0" aria-hidden />
          <span className="truncate">{label}</span>
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="size-5 shrink-0"
          onClick={onRemove}
          aria-label={msg('managedAgents.agents.createDialog.removeItem', 'Remove {name}', { name: label })}
        >
          <X className="size-3" aria-hidden />
        </Button>
      </Badge>
      {query.data?.multiagent ? (
        <p className="w-full text-sm text-destructive">
          {msg(
            'managedAgents.agents.createDialog.nestedSubagent',
            '{agent} has multiagent settings of its own, so it can’t be used here.',
            { agent: label },
          )}
        </p>
      ) : null}
      {open ? (
        <AgentQuickView
          agentId={id}
          version={version}
          agentIds={[id]}
          workspaceId={workspaceId}
          orgUuid={orgUuid ?? ''}
          onSelect={() => {}}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  );
}
