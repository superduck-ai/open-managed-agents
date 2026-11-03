import { useQuery } from '@tanstack/react-query';

import { anthropicRequestHeaders } from '../../shared/api/anthropic';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { evaluationErrorMessage } from './api';
import { catalogModels } from '../llm-providers/catalog';
import { listLLMProviders, type LLMProvider } from '../llm-providers/api';
import type { SessionApiResponse } from '../managed-agents/types';
import { useEvaluationWorkspace } from './useEvaluationDatasets';

type SessionOption = { id: string; title: string };

export function useWorkspaceSessions(enabled: boolean) {
  const workspace = useEvaluationWorkspace();
  return useQuery({
    queryKey: ['evaluation-sessions', workspace.workspaceId],
    enabled: enabled && workspace.workspaceReady,
    queryFn: async () => {
      const params = new URLSearchParams({ beta: 'true', limit: '50' });
      const response = await globalThis.fetch(`/v1/sessions?${params}`, {
        headers: requestHeaders(anthropicRequestHeaders({ workspaceId: workspace.workspaceId })),
      });
      if (!response.ok) {
        throw new Error('Could not load sessions');
      }
      const page = (await response.json()) as { data?: SessionApiResponse[] };
      return sessionOptions(page.data);
    },
  });
}

export function useEvaluatorModels(enabled: boolean) {
  const workspace = useEvaluationWorkspace();
  return useQuery({
    queryKey: ['evaluation-evaluator-models', workspace.orgUuid, workspace.workspaceId],
    enabled: enabled && workspace.workspaceReady,
    queryFn: async () => {
      const providers = await listLLMProviders(workspace.orgUuid ?? '', workspace.workspaceId);
      return catalogModels(providerList(providers)).map((model) => model.qualifiedId);
    },
  });
}

export function SessionSelect({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const { msg } = useI18n();
  const query = useWorkspaceSessions(true);
  const sessions = query.data ?? [];
  if (query.isLoading) {
    return <p className="text-sm text-muted-foreground">{msg('common.loading', 'Loading...')}</p>;
  }
  if (query.isError) {
    return <p className="text-sm text-destructive">{evaluationErrorMessage(query.error)}</p>;
  }
  if (sessions.length === 0) {
    return <p className="text-sm text-muted-foreground">{msg('evaluations.noSessions', 'No sessions yet.')}</p>;
  }
  return (
    <RecordSelect
      label={msg('evaluations.sessionId', 'Session')}
      placeholder={msg('evaluations.selectSession', 'Select a session')}
      value={value}
      options={sessions.map((session) => ({ value: session.id, label: session.title || session.id }))}
      onChange={onChange}
    />
  );
}

export function SessionChecklist({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const { msg } = useI18n();
  const query = useWorkspaceSessions(true);
  const sessions = query.data ?? [];
  if (query.isLoading) {
    return <p className="text-sm text-muted-foreground">{msg('common.loading', 'Loading...')}</p>;
  }
  if (sessions.length === 0) {
    return <p className="text-sm text-muted-foreground">{msg('evaluations.noSessions', 'No sessions yet.')}</p>;
  }
  return (
    <div
      className="grid max-h-48 gap-2 overflow-y-auto"
      role="group"
      aria-label={msg('evaluations.sessionIds', 'Sessions')}
    >
      {sessions.map((session) => {
        const checked = value.includes(session.id);
        const label = session.title || session.id;
        return (
          <Label key={session.id} className="items-start gap-2 font-normal">
            <Checkbox
              checked={checked}
              aria-label={label}
              onCheckedChange={(next) => onChange(toggleId(value, session.id, next === true))}
            />
            <span className="min-w-0 truncate">{label}</span>
          </Label>
        );
      })}
    </div>
  );
}

export function ModelSelect({ value, onChange }: { value: string; onChange: (model: string) => void }) {
  const { msg } = useI18n();
  const query = useEvaluatorModels(true);
  const models = query.data ?? [];
  if (query.isLoading) {
    return <p className="text-sm text-muted-foreground">{msg('common.loading', 'Loading...')}</p>;
  }
  if (models.length === 0) {
    return <p className="text-sm text-muted-foreground">{msg('evaluations.noModels', 'No models configured.')}</p>;
  }
  return (
    <RecordSelect
      label={msg('evaluations.model', 'Model')}
      placeholder={msg('evaluations.selectModel', 'Select a model')}
      value={value}
      options={models.map((model) => ({ value: model, label: model }))}
      onChange={onChange}
    />
  );
}

function RecordSelect({
  label,
  placeholder,
  value,
  options,
  onChange,
}: {
  label: string;
  placeholder: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
}) {
  return (
    <div className="grid min-w-0 gap-2">
      <span className="text-sm font-medium">{label}</span>
      <Select
        value={value}
        items={options}
        onValueChange={(next) => {
          if (next) {
            onChange(next);
          }
        }}
      >
        <SelectTrigger aria-label={label} className="w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value} label={option.label}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

export function NativeIdSelect({
  id,
  label,
  value,
  placeholder,
  options,
  onChange,
  required = false,
  bare = false,
  marked = false,
}: {
  id: string;
  label: string;
  value: string;
  placeholder: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
  required?: boolean;
  bare?: boolean;
  marked?: boolean;
}) {
  return (
    <div className="grid gap-2">
      {bare ? null : (
        <div className="flex items-center gap-0.5">
          {required || marked ? (
            <span className="text-destructive" aria-hidden>
              *
            </span>
          ) : null}
          <Label htmlFor={id}>{label}</Label>
        </div>
      )}
      <select
        id={id}
        aria-label={label}
        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
        value={value}
        required={required}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">{placeholder}</option>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
  );
}

export type EvaluatorMountDraft = { fileId: string; path: string };

const evaluatorAgentsPath = '/v1/agents?beta=true&limit=50';
const evaluatorEnvironmentsPath = '/v1/environments?beta=true&limit=50';

export function emptyEvaluatorMount(): EvaluatorMountDraft {
  return { fileId: '', path: '' };
}

export function evaluatorMountDrafts(resources?: Array<{ file_id: string; path: string }>): EvaluatorMountDraft[] {
  if (!resources?.length) {
    return [emptyEvaluatorMount()];
  }
  return resources.map((resource) => ({ fileId: resource.file_id, path: resource.path }));
}

export function evaluatorMountPayload(rows: EvaluatorMountDraft[]) {
  const mounts = rows
    .map((row) => ({ file_id: row.fileId.trim(), path: row.path.trim() }))
    .filter((row) => row.file_id.length > 0 || row.path.length > 0);
  return mounts.length > 0 ? mounts : undefined;
}

export function AgentSessionFields({
  agentId,
  environmentId,
  mounts,
  onAgentId,
  onEnvironmentId,
  onMounts,
}: {
  agentId: string;
  environmentId: string;
  mounts: EvaluatorMountDraft[];
  onAgentId: (value: string) => void;
  onEnvironmentId: (value: string) => void;
  onMounts: (mounts: EvaluatorMountDraft[]) => void;
}) {
  const { msg } = useI18n();
  const agents = useNamedRecords(evaluatorAgentsPath);
  const environments = useNamedRecords(evaluatorEnvironmentsPath);
  const agentOptions = (agents.data ?? []).map((record) => ({ value: record.id, label: record.name }));
  const environmentOptions = (environments.data ?? []).map((record) => ({ value: record.id, label: record.name }));

  function updateMount(index: number, patch: Partial<EvaluatorMountDraft>) {
    onMounts(mounts.map((mount, mountIndex) => (mountIndex === index ? { ...mount, ...patch } : mount)));
  }

  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div>
        <div className="text-sm font-medium">{msg('evaluations.agentSessionConfig', 'Agent session')}</div>
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.scoringAgentHint', 'Scoring starts a session on this Agent.')}
        </p>
      </div>
      <NativeIdSelect
        id="evaluation-evaluator-agent"
        label={msg('evaluations.scoringAgent', 'Scoring Agent')}
        value={agentId}
        placeholder={agents.data?.length ? msg('evaluations.selectAgent', 'Select an Agent') : msg('evaluations.noAgents', 'No Agents yet.')}
        options={agentOptions}
        onChange={onAgentId}
        required
      />
      <NativeIdSelect
        id="evaluation-evaluator-environment"
        label={msg('evaluations.environmentOptional', 'Environment (optional)')}
        value={environmentId}
        placeholder={msg('evaluations.noEnvironment', 'None')}
        options={environmentOptions}
        onChange={onEnvironmentId}
      />
      <div className="grid gap-2">
        <span className="text-sm font-medium">{msg('evaluations.evaluatorFiles', 'Files (optional)')}</span>
        {mounts.map((mount, index) => (
          <div key={index} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2">
            <Input
              aria-label={msg('evaluations.fileId', 'File ID')}
              value={mount.fileId}
              placeholder={msg('evaluations.fileId', 'File ID')}
              onChange={(event) => updateMount(index, { fileId: event.target.value })}
            />
            <Input
              aria-label={msg('evaluations.mountPath', 'Path')}
              value={mount.path}
              placeholder={msg('evaluations.mountPathPlaceholder', '/data/filename')}
              onChange={(event) => updateMount(index, { path: event.target.value })}
            />
            <Button
              type="button"
              variant="outline"
              onClick={() => onMounts(mounts.length === 1 ? [emptyEvaluatorMount()] : mounts.filter((_, mountIndex) => mountIndex !== index))}
            >
              {msg('common.delete', 'Delete')}
            </Button>
          </div>
        ))}
        <Button type="button" variant="outline" className="w-fit" onClick={() => onMounts([...mounts, emptyEvaluatorMount()])}>
          {msg('evaluations.addFile', 'Add file')}
        </Button>
      </div>
    </div>
  );
}

export function useNamedRecords(path: string) {
  const workspace = useEvaluationWorkspace();
  return useQuery({
    queryKey: ['evaluation-named-records', path, workspace.workspaceId],
    enabled: workspace.workspaceReady && path.length > 0,
    queryFn: async () => {
      const response = await globalThis.fetch(path, {
        headers: requestHeaders(anthropicRequestHeaders({ workspaceId: workspace.workspaceId })),
      });
      if (!response.ok) {
        throw new Error('Could not load records');
      }
      const page = (await response.json()) as { data?: unknown };
      return namedRecords(page.data);
    },
  });
}

function namedRecords(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object' || !('id' in item) || typeof item.id !== 'string' || item.id.length === 0) {
      return [];
    }
    const name = 'name' in item && typeof item.name === 'string' && item.name.length > 0 ? item.name : item.id;
    return [{ id: item.id, name }];
  });
}

function requestHeaders(headers: Record<string, string | null>) {
  return Object.fromEntries(Object.entries(headers).filter((entry): entry is [string, string] => entry[1] !== null));
}

function sessionOptions(value: unknown): SessionOption[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object' || !('id' in item) || typeof item.id !== 'string') {
      return [];
    }
    const title = 'title' in item && typeof item.title === 'string' ? item.title : '';
    return [{ id: item.id, title }];
  });
}

function providerList(value: unknown): LLMProvider[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object' || !('slug' in item) || typeof item.slug !== 'string') {
      return [];
    }
    const models = providerModels(item);
    if (!models) {
      return [];
    }
    return [{ ...(item as LLMProvider), models }];
  });
}

function providerModels(item: object): LLMProvider['models'] | null {
  if ('models' in item && Array.isArray(item.models)) {
    return item.models.flatMap((model) => {
      if (model && typeof model === 'object' && 'id' in model && typeof model.id === 'string') {
        return [{ id: model.id }];
      }
      return [];
    });
  }
  if ('model_ids' in item && Array.isArray(item.model_ids)) {
    return item.model_ids.flatMap((id) => (typeof id === 'string' ? [{ id }] : []));
  }
  return null;
}

function toggleId(current: string[], id: string, checked: boolean) {
  if (checked) {
    return current.includes(id) || current.length >= 50 ? current : [...current, id];
  }
  return current.filter((item) => item !== id);
}
