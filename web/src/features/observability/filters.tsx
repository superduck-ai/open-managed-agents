import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { listObservabilityAgents, listObservabilityAgentVersions, listObservabilitySessions } from './api';
import { parseCsvList, type ObservabilityFilters } from './model';
import { ScopeCombobox } from './ScopeCombobox';
import { VersionCombobox } from './VersionCombobox';
import type { ObservabilityScope, ObservabilityTabId } from './types';

const SCOPE_OPTIONS_STALE_MS = 60_000;

export function ObservabilityFiltersBar({
  filters,
  onChange,
  scope,
  tab,
}: {
  filters: ObservabilityFilters;
  onChange: (next: ObservabilityFilters) => void;
  scope: ObservabilityScope;
  tab: ObservabilityTabId;
}) {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  const showAgent = scope.kind === 'workspace';
  const showVersion = scope.kind === 'agent';
  const showSession = scope.kind !== 'session';
  const showModel = tab === 'model';
  const showTool = tab === 'tool';
  const showTraceFilters = tab === 'traces';
  const scopedAgentId = scope.kind === 'agent' ? scope.agentId : filters.agentId;
  const agentsQuery = useQuery({
    queryKey: ['observability', 'agents', activeWorkspaceId],
    queryFn: () => listObservabilityAgents(activeWorkspaceId),
    enabled: showAgent,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: SCOPE_OPTIONS_STALE_MS,
  });
  const sessionsQuery = useQuery({
    queryKey: ['observability', 'sessions', activeWorkspaceId, scopedAgentId],
    queryFn: () => listObservabilitySessions(activeWorkspaceId, scopedAgentId || undefined),
    enabled: showSession,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: SCOPE_OPTIONS_STALE_MS,
  });
  const versionsQuery = useQuery({
    queryKey: ['observability', 'agent-versions', activeWorkspaceId, scopedAgentId],
    queryFn: () => listObservabilityAgentVersions(scopedAgentId, activeWorkspaceId),
    enabled: showVersion && Boolean(scopedAgentId),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: SCOPE_OPTIONS_STALE_MS,
  });
  const statusOptions = [
    { value: 'all', label: msg('observability.filter.status.all', 'All statuses') },
    { value: 'ok', label: msg('observability.filter.status.ok', 'OK') },
    { value: 'error', label: msg('observability.filter.status.error', 'Error') },
  ];
  return (
    <>
      {showAgent ? (
        <ScopeCombobox
          label={msg('observability.filter.agent', 'Agent')}
          value={filters.agentId}
          allLabel={msg('observability.filter.allAgents', 'All agents')}
          searchPlaceholder={msg('observability.filter.searchAgent', 'Search agents')}
          emptyLabel={msg('observability.filter.emptyAgents', 'No agents found')}
          clearLabel={msg('observability.filter.clearAgent', 'Clear agent')}
          options={agentsQuery.data ?? []}
          loading={agentsQuery.isPending}
          onChange={(agentId) => onChange({ ...filters, agentId, sessionId: '' })}
        />
      ) : null}
      {showVersion ? (
        <VersionCombobox
          label={msg('observability.filter.version', 'Agent version')}
          values={filters.agentVersions}
          allLabel={msg('observability.filter.allVersions', 'All versions')}
          searchPlaceholder={msg('observability.filter.searchVersion', 'Search version')}
          emptyLabel={msg('observability.filter.emptyVersions', 'No versions found')}
          clearLabel={msg('observability.filter.clearVersion', 'Clear version')}
          options={versionsQuery.data ?? []}
          loading={versionsQuery.isPending}
          onChange={(agentVersions) => onChange({ ...filters, agentVersions })}
        />
      ) : null}
      {showSession ? (
        <ScopeCombobox
          label={msg('observability.filter.session', 'Session')}
          value={filters.sessionId}
          allLabel={msg('observability.filter.allSessions', 'All sessions')}
          searchPlaceholder={msg('observability.filter.searchSession', 'Search sessions')}
          emptyLabel={msg('observability.filter.emptySessions', 'No sessions found')}
          clearLabel={msg('observability.filter.clearSession', 'Clear session')}
          options={sessionsQuery.data ?? []}
          loading={sessionsQuery.isPending}
          onChange={(sessionId) => onChange({ ...filters, sessionId })}
        />
      ) : null}
      {showModel ? (
        <CommitInput
          value={filters.models.join(', ')}
          ariaLabel={msg('observability.filter.model', 'Models')}
          placeholder={msg('observability.filter.modelPlaceholder', 'model-a, model-b')}
          className="h-7 w-56"
          onCommit={(raw) => onChange({ ...filters, models: parseCsvList(raw) })}
        />
      ) : null}
      {showTool ? (
        <CommitInput
          value={filters.tools.join(', ')}
          ariaLabel={msg('observability.filter.tool', 'Tools')}
          placeholder={msg('observability.filter.toolPlaceholder', 'Bash, Read')}
          className="h-7 w-56"
          onCommit={(raw) => onChange({ ...filters, tools: parseCsvList(raw) })}
        />
      ) : null}
      {showTraceFilters ? (
        <>
          <CommitInput
            value={filters.traceId}
            ariaLabel={msg('observability.filter.traceId', 'Trace ID')}
            placeholder={msg('observability.filter.traceIdPlaceholder', 'Trace ID')}
            className="h-7 w-52 font-mono"
            onCommit={(raw) => onChange({ ...filters, traceId: raw.trim() })}
          />
          <Select
            value={filters.status || 'all'}
            items={statusOptions}
            onValueChange={(next) => {
              if (next === null) {
                return;
              }
              onChange({ ...filters, status: next === 'all' ? '' : (next as ObservabilityFilters['status']) });
            }}
          >
            <SelectTrigger aria-label={msg('observability.filter.status', 'Status')} size="sm" className="min-w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {statusOptions.map((option) => (
                <SelectItem key={option.value} value={option.value} label={option.label}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>
      ) : null}
    </>
  );
}

// 输入过程只改本地草稿，失焦或回车才提交。避免受控值经过 parse 归一化后吞掉
// 用户刚敲的逗号/空格，也避免每个键击都触发一轮面板查询。
function CommitInput({
  value,
  onCommit,
  ariaLabel,
  placeholder,
  className,
}: {
  value: string;
  onCommit: (raw: string) => void;
  ariaLabel: string;
  placeholder: string;
  className?: string;
}) {
  const [draft, setDraft] = useState(value);
  const [committed, setCommitted] = useState(value);
  if (value !== committed) {
    setCommitted(value);
    setDraft(value);
  }
  const commit = () => {
    if (draft !== value) {
      onCommit(draft);
    }
  };
  return (
    <Input
      value={draft}
      aria-label={ariaLabel}
      placeholder={placeholder}
      className={className}
      onChange={(event) => setDraft(event.currentTarget.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === 'Enter') {
          commit();
        }
      }}
    />
  );
}
