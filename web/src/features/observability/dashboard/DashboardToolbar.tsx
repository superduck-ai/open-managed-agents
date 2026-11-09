import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { ChevronDown } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { listAgentVersions, retrieveAgent } from '../../managed-agents/api';
import { listAgentPickerPage } from '../../managed-agents/agents/agent-picker-api';
import { ScopeCombobox, type ScopeOption } from './ScopeCombobox';

function AgentVariable({
  workspaceId,
  agentId,
  onChange,
}: {
  workspaceId: string;
  agentId: string;
  onChange: (agentId: string) => void;
}) {
  const { msg } = useI18n();
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [search]);
  const agents = useQuery({
    queryKey: ['observability-agents', workspaceId, debouncedSearch],
    queryFn: () => listAgentPickerPage(workspaceId, debouncedSearch),
    enabled: !!workspaceId,
    placeholderData: keepPreviousData,
  });
  const listed = agents.data?.data ?? [];
  const selected = useQuery({
    queryKey: ['observability-agent', workspaceId, agentId],
    queryFn: () => retrieveAgent(agentId, workspaceId),
    enabled: !!agentId && !agents.isPending && !listed.some((agent) => agent.id === agentId),
    retry: false,
  });
  const option = (agent: { id: string; name?: string | null }) => ({
    value: agent.id,
    label: agent.name || agent.id,
    description: agent.id,
  });
  return (
    <ScopeCombobox
      label={msg('observability.agent', 'Agent')}
      value={agentId}
      allLabel={msg('observability.allAgents', 'All agents')}
      searchPlaceholder={msg('observability.searchAgents', 'Search agents')}
      emptyLabel={msg('observability.noAgents', 'No agents found')}
      options={listed.map(option)}
      selectedOption={selected.data && option(selected.data)}
      loading={agents.isPending}
      onSearchChange={setSearch}
      onChange={onChange}
    />
  );
}

export function FilterVariable({
  kind,
  value,
  options,
  onChange,
}: {
  kind: 'models' | 'tools';
  value: string;
  options: string[];
  onChange: (value: string) => void;
}) {
  const { msg } = useI18n();
  const models = kind === 'models';
  return (
    <ScopeCombobox
      label={models ? msg('observability.model', 'Model') : msg('observability.tool', 'Tool')}
      value={value}
      allLabel={models ? msg('observability.allModels', 'All models') : msg('observability.allTools', 'All tools')}
      searchPlaceholder={
        models ? msg('observability.searchModels', 'Search models') : msg('observability.searchTools', 'Search tools')
      }
      emptyLabel={msg('observability.noOptions', 'No matches')}
      options={options.map((option) => ({ value: option, label: option }))}
      onChange={onChange}
    />
  );
}

export function SessionVariable({
  sessionId,
  options,
  loading,
  onChange,
}: {
  sessionId: string;
  options: ScopeOption[];
  loading: boolean;
  onChange: (sessionId: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <ScopeCombobox
      label={msg('observability.session', 'Session')}
      value={sessionId}
      allLabel={msg('observability.allSessions', 'All sessions')}
      searchPlaceholder={msg('observability.searchSessions', 'Search or paste a session ID (sesn_…)')}
      emptyLabel={msg('observability.noSessions', 'No sessions in this time range')}
      options={options}
      loading={loading}
      customPrefix="sesn_"
      onChange={onChange}
    />
  );
}

function VersionSelect({
  workspaceId,
  agentId,
  versions,
  onChange,
}: {
  workspaceId: string;
  agentId: string;
  versions: number[];
  onChange: (versions: number[]) => void;
}) {
  const { msg } = useI18n();
  const history = useQuery({
    queryKey: ['observability-agent-versions', workspaceId, agentId],
    queryFn: () => listAgentVersions(agentId, workspaceId),
    enabled: !!workspaceId && !!agentId,
  });
  const options = [...new Set((history.data?.data ?? []).map((agent) => agent.version))].sort((a, b) => b - a);
  const label = versions.length
    ? versions.map((version) => `v${version}`).join(', ')
    : msg('observability.allVersions', 'All versions');
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="outline" className="w-40 justify-between font-normal" />}
        aria-label={msg('observability.version', 'Version')}
      >
        <span className="truncate">{label}</span>
        <ChevronDown className="size-4 opacity-60" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        {options.map((version) => (
          <DropdownMenuCheckboxItem
            key={version}
            checked={versions.includes(version)}
            onCheckedChange={(checked) =>
              onChange(
                checked
                  ? [...versions, version].sort((a, b) => a - b)
                  : versions.filter((current) => current !== version),
              )
            }
          >
            v{version}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function DashboardToolbar({
  workspaceId,
  agentId,
  fixedAgent,
  versions,
  session,
  filter,
  timeControls,
  onAgentChange,
  onVersionsChange,
}: {
  workspaceId: string;
  agentId: string;
  fixedAgent: boolean;
  versions: number[];
  session: ReactNode;
  filter?: ReactNode;
  timeControls: ReactNode;
  onAgentChange: (agentId: string) => void;
  onVersionsChange: (versions: number[]) => void;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        {!fixedAgent && <AgentVariable workspaceId={workspaceId} agentId={agentId} onChange={onAgentChange} />}
        {session}
        {agentId && (
          <VersionSelect workspaceId={workspaceId} agentId={agentId} versions={versions} onChange={onVersionsChange} />
        )}
        {filter}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2">{timeControls}</div>
    </div>
  );
}
