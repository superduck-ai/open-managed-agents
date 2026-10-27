import { Check, ChevronDown, Info, X } from 'lucide-react';
import { useState, type ChangeEvent, type FormEvent, type ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button, ButtonLink } from '../../shared/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '../../shared/ui/popover';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import {
  evaluationErrorMessage,
  type EvaluationDataset,
  type EvaluationEvaluator,
  type EvaluationEvaluatorPreset,
} from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { NativeIdSelect, useNamedRecords } from './EvaluationPickers';
import { parseExperimentEnv, parseExperimentResources } from './experimentDraft';
import { evaluationExperimentHref, evaluationExperimentsHref, useEvaluationDatasetList } from './useEvaluationDatasets';
import { useEvaluationEvaluatorList, useEvaluationEvaluatorPresetList } from './useEvaluationEvaluators';
import { useCreateEvaluationExperiment } from './useEvaluationExperiments';

type ExperimentDraft = {
  name: string;
  desc: string;
  datasetId: string;
  datasetVersionId: string;
  agentId: string;
  agentVersion: string;
  environmentId: string;
  timeoutSeconds: string;
  env: string;
  resources: string;
  evaluatorIds: string[];
};

const emptyDraft: ExperimentDraft = {
  name: '',
  desc: '',
  datasetId: '',
  datasetVersionId: '',
  agentId: '',
  agentVersion: '',
  environmentId: '',
  timeoutSeconds: '1200',
  env: '',
  resources: '',
  evaluatorIds: [],
};

const agentsPath = '/v1/agents?beta=true&limit=50';
const environmentsPath = '/v1/environments?beta=true&limit=50';

export function ExperimentNewPage() {
  const { msg } = useI18n();
  const { workspace, query: datasets } = useEvaluationDatasetList();
  const evaluators = useEvaluationEvaluatorList();
  const presets = useEvaluationEvaluatorPresetList();
  const agents = useNamedRecords(agentsPath);
  const environments = useNamedRecords(environmentsPath);
  const create = useCreateEvaluationExperiment();
  const [draft, setDraft] = useState(emptyDraft);
  const [envError, setEnvError] = useState(false);
  const [resourcesError, setResourcesError] = useState(false);
  const [envOpen, setEnvOpen] = useState(false);
  const [resourcesOpen, setResourcesOpen] = useState(false);
  const ready =
    draft.datasetVersionId.length > 0 &&
    draft.agentId.length > 0 &&
    draft.environmentId.length > 0 &&
    draft.evaluatorIds.length > 0;

  function setField(key: 'name' | 'desc' | 'agentVersion' | 'timeoutSeconds' | 'env' | 'resources') {
    return (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      const value = event.target.value;
      setDraft((current) => ({ ...current, [key]: value }));
    };
  }

  function setPick(key: 'datasetVersionId' | 'agentId' | 'environmentId') {
    return (value: string) => setDraft((current) => ({ ...current, [key]: value }));
  }

  function onDataset(value: string) {
    setDraft((current) => ({ ...current, datasetId: value, datasetVersionId: '' }));
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const agentVersion = draft.agentVersion.trim();
    const env = parseExperimentEnv(draft.env);
    const resources = parseExperimentResources(draft.resources);
    setEnvError(env.error);
    setResourcesError(resources.error);
    if (env.error || resources.error || !ready) {
      return;
    }
    create.mutate(
      {
        name: draft.name,
        desc: draft.desc,
        dataset_version_id: draft.datasetVersionId,
        agent_id: draft.agentId,
        ...(agentVersion ? { agent_version: Number(agentVersion) } : {}),
        environment_id: draft.environmentId,
        timeout_seconds: Number(draft.timeoutSeconds),
        ...(env.env ? { env: env.env } : {}),
        ...(resources.resources ? { resources: resources.resources } : {}),
        evaluator_ids: draft.evaluatorIds,
      },
      {
        onSuccess: (experiment) => {
          window.location.assign(evaluationExperimentHref(workspace.workspaceId, experiment.id));
        },
      },
    );
  }

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <div className="mx-auto w-full max-w-3xl">
        <header className="mb-4">
          <h1 className="text-2xl font-semibold tracking-tight">
            {msg('evaluations.createExperiment', 'New experiment')}
          </h1>
          <p className="mt-1 text-sm leading-5 text-muted-foreground">
            {msg(
              'evaluations.experimentStepsNote',
              'Pick a dataset, an agent, and evaluators. The run scores every case.',
            )}
          </p>
        </header>
        <form className="grid gap-4" onSubmit={onSubmit}>
          <StartNote />
          <NameCard name={draft.name} onChange={setField('name')} />
          <DatasetStep
            draft={draft}
            datasets={datasets.data?.data ?? []}
            error={datasets.error ? evaluationErrorMessage(datasets.error) : ''}
            onDataset={onDataset}
            onVersion={setPick('datasetVersionId')}
          />
          <AgentStep
            draft={draft}
            agents={(agents.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            environments={(environments.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            agentError={agents.error ? evaluationErrorMessage(agents.error) : ''}
            environmentError={environments.error ? evaluationErrorMessage(environments.error) : ''}
            envError={envError}
            resourcesError={resourcesError}
            envOpen={envOpen}
            resourcesOpen={resourcesOpen}
            onOpenEnv={() => setEnvOpen(true)}
            onOpenResources={() => setResourcesOpen(true)}
            onField={setField}
            onAgent={setPick('agentId')}
            onEnvironment={setPick('environmentId')}
          />
          <EvaluatorStep
            evaluators={bindableEvaluators(evaluators.query.data?.data, presets.query.data?.data)}
            pending={evaluators.query.isLoading || presets.query.isLoading}
            selected={draft.evaluatorIds}
            error={evaluatorListError(evaluators.query.error, presets.query.error)}
            onChange={(evaluatorIds) => setDraft((current) => ({ ...current, evaluatorIds }))}
          />
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <div className="flex items-center justify-center gap-3 pt-2">
            <Button type="submit" className="h-9" disabled={create.isPending || !ready}>
              {msg('evaluations.startExperiment', 'New experiment')}
            </Button>
            <ButtonLink href={evaluationExperimentsHref(workspace.workspaceId)} variant="outline">
              {msg('common.cancel', 'Cancel')}
            </ButtonLink>
          </div>
        </form>
      </div>
    </section>
  );
}

function StartNote() {
  const { msg } = useI18n();
  return (
    <div className="flex gap-3 rounded-lg border border-sky-200 bg-sky-50 px-4 py-3 text-sm text-sky-950">
      <Info className="mt-0.5 size-4 shrink-0" aria-hidden />
      <div>
        <p className="font-medium">{msg('evaluations.experimentStartTitle', 'Three steps start an experiment')}</p>
        <p className="mt-1 text-sky-900">
          {msg(
            'evaluations.experimentStartBody',
            'Choose a dataset, an agent, and evaluators. The system runs the trial and scores it.',
          )}
        </p>
      </div>
    </div>
  );
}

function NameCard({ name, onChange }: { name: string; onChange: (event: ChangeEvent<HTMLInputElement>) => void }) {
  const { msg } = useI18n();
  return (
    <section className="grid gap-1.5 rounded-lg border px-5 py-5">
      <RequiredLabel htmlFor="name">{msg('evaluations.experimentName', 'Name')}</RequiredLabel>
      <Input
        id="name"
        value={name}
        maxLength={50}
        placeholder={msg('evaluations.experimentNamePlaceholder', 'Experiment name')}
        onChange={(event) => {
          event.target.value = event.target.value.slice(0, 50);
          onChange(event);
        }}
        required
      />
      <p className="text-xs text-muted-foreground">{name.length}/50</p>
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.experimentNameHint',
          'Identifies this experiment in the list. The name must be unique in this space and mode.',
        )}
      </p>
    </section>
  );
}

function DatasetStep({
  draft,
  datasets,
  error,
  onDataset,
  onVersion,
}: {
  draft: ExperimentDraft;
  datasets: EvaluationDataset[];
  error: string;
  onDataset: (value: string) => void;
  onVersion: (value: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <StepCard
      index="1"
      title={msg('evaluations.stepDataset', '1. Dataset')}
      hint={msg('evaluations.stepDatasetHint', 'Choose the dataset for this experiment.')}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <NativeIdSelect
          id="dataset_id"
          label={msg('evaluations.datasetField', 'Dataset')}
          placeholder={msg('evaluations.selectDataset', 'Select a dataset')}
          marked
          value={draft.datasetId}
          options={datasetOptions(datasets)}
          onChange={onDataset}
        />
        <NativeIdSelect
          id="dataset_version_id"
          label={msg('evaluations.datasetVersion', 'Dataset version')}
          placeholder={
            draft.datasetId
              ? msg('evaluations.selectDatasetVersion', 'Select a dataset version')
              : msg('evaluations.pickVersionFirst', 'Select a dataset first')
          }
          value={draft.datasetVersionId}
          options={datasetVersionOptions(datasets, draft.datasetId)}
          onChange={onVersion}
          required
        />
      </div>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
    </StepCard>
  );
}

function AgentStep({
  draft,
  agents,
  environments,
  agentError,
  environmentError,
  envError,
  resourcesError,
  envOpen,
  resourcesOpen,
  onOpenEnv,
  onOpenResources,
  onField,
  onAgent,
  onEnvironment,
}: {
  draft: ExperimentDraft;
  agents: Array<{ value: string; label: string }>;
  environments: Array<{ value: string; label: string }>;
  agentError: string;
  environmentError: string;
  envError: boolean;
  resourcesError: boolean;
  envOpen: boolean;
  resourcesOpen: boolean;
  onOpenEnv: () => void;
  onOpenResources: () => void;
  onField: (
    key: 'agentVersion' | 'timeoutSeconds' | 'env' | 'resources',
  ) => (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => void;
  onAgent: (value: string) => void;
  onEnvironment: (value: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <StepCard
      index="2"
      title={msg('evaluations.stepAgent', '2. Agent')}
      hint={msg('evaluations.stepAgentHint', 'Choose the Agent to evaluate.')}
    >
      <NativeIdSelect
        id="agent_id"
        label={msg('evaluations.agentId', 'Agent')}
        placeholder={msg('evaluations.selectAgent', 'Select an agent')}
        value={draft.agentId}
        options={agents}
        onChange={onAgent}
        required
      />
      {agentError ? <p className="text-sm text-destructive">{agentError}</p> : null}
      <NativeIdSelect
        id="environment_id"
        label={msg('evaluations.environmentId', 'Environment')}
        placeholder={msg('evaluations.selectEnvironment', 'Select an environment')}
        value={draft.environmentId}
        options={environments}
        onChange={onEnvironment}
        required
      />
      {environmentError ? <p className="text-sm text-destructive">{environmentError}</p> : null}
      <Field
        label={msg('evaluations.agentTimeout', 'Timeout (seconds)')}
        name="timeout_seconds"
        value={draft.timeoutSeconds}
        onChange={onField('timeoutSeconds')}
      />
      <OptionalEditor
        title={msg('evaluations.envOptional', 'Environment variables')}
        hint={msg('evaluations.envPassedHint', 'Passed into each Managed session for this experiment.')}
        addLabel={msg('evaluations.addEnv', 'Add environment variable')}
        open={envOpen}
        onOpen={onOpenEnv}
      >
        <EnvField value={draft.env} invalid={envError} onChange={onField('env')} />
      </OptionalEditor>
      <OptionalEditor
        title={msg('evaluations.resourcesOptional', 'Resources')}
        hint={msg('evaluations.resourcesMountHint', 'Mount resources for the two Managed sessions in this experiment.')}
        addLabel={msg('evaluations.addResource', 'Add resource')}
        open={resourcesOpen}
        onOpen={onOpenResources}
        empty={msg('evaluations.resourcesEmpty', 'No resources configured')}
      >
        <ResourcesField value={draft.resources} invalid={resourcesError} onChange={onField('resources')} />
      </OptionalEditor>
    </StepCard>
  );
}

function EvaluatorStep({
  evaluators,
  pending,
  selected,
  error,
  onChange,
}: {
  evaluators: BindableEvaluator[];
  pending: boolean;
  selected: string[];
  error: string;
  onChange: (ids: string[]) => void;
}) {
  const { msg } = useI18n();
  return (
    <StepCard
      index="3"
      title={msg('evaluations.stepEvaluators', '3. Evaluators')}
      hint={msg('evaluations.stepEvaluatorsHint', 'Choose at least one evaluator to score the run.')}
    >
      <RequiredLabel htmlFor="experiment-evaluators">{msg('evaluations.evaluatorIds', 'Evaluators')}</RequiredLabel>
      {evaluators.length === 0 && !pending ? (
        <p className="text-sm text-muted-foreground">{msg('evaluations.noEvaluatorsToBind', 'No evaluators yet.')}</p>
      ) : (
        <EvaluatorMultiSelect evaluators={evaluators} selected={selected} onChange={onChange} />
      )}
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.evaluatorFrozenHint',
          'Evaluator definitions stay fixed for this experiment, so scores stay comparable.',
        )}
      </p>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
    </StepCard>
  );
}

function EvaluatorMultiSelect({
  evaluators,
  selected,
  onChange,
}: {
  evaluators: BindableEvaluator[];
  selected: string[];
  onChange: (ids: string[]) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const visible = filterEvaluators(evaluators, query);
  const chosen = evaluators.filter((item) => selected.includes(item.id));
  return (
    <div className="grid gap-2">
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) {
            setQuery('');
          }
        }}
      >
        <PopoverTrigger
          render={
            <Button
              id="experiment-evaluators"
              type="button"
              variant="outline"
              aria-expanded={open}
              aria-label={msg('evaluations.selectEvaluators', 'Select evaluators')}
              className="h-9 w-full justify-between px-3 font-normal text-muted-foreground"
            />
          }
        >
          <span>{msg('evaluations.selectEvaluators', 'Select evaluators')}</span>
          <ChevronDown className="size-4" aria-hidden />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-(--anchor-width) gap-0 overflow-hidden p-0">
          <div className="border-b px-3 py-2">
            <Input
              aria-label={msg('evaluations.searchEvaluators', 'Search evaluators')}
              className="h-8 border-0 px-0 shadow-none focus-visible:ring-0"
              placeholder={msg('evaluations.searchEvaluators', 'Search evaluators')}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          <div className="max-h-64 overflow-auto p-1">
            {visible.length === 0 ? (
              <p className="px-2 py-6 text-center text-sm text-muted-foreground">
                {msg('evaluations.evaluatorsNoMatch', 'No matching evaluators.')}
              </p>
            ) : (
              visible.map((evaluator) => (
                <EvaluatorOption
                  key={evaluator.id}
                  evaluator={evaluator}
                  checked={selected.includes(evaluator.id)}
                  onChange={(checked) => onChange(toggleEvaluator(selected, evaluator.id, checked))}
                />
              ))
            )}
          </div>
          <p className="border-t px-3 py-2 text-sm font-medium">
            {msg('evaluations.selectedEvaluators', 'Selected {count}', { count: selected.length })}
          </p>
        </PopoverContent>
      </Popover>
      {chosen.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {chosen.map((evaluator) => (
            <EvaluatorChip
              key={evaluator.id}
              label={evaluatorOptionLabel(evaluator, msg)}
              removeLabel={msg('evaluations.removeEvaluator', 'Remove {name}', { name: evaluator.name })}
              onRemove={() => onChange(toggleEvaluator(selected, evaluator.id, false))}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function EvaluatorOption({
  evaluator,
  checked,
  onChange,
}: {
  evaluator: BindableEvaluator;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const { msg } = useI18n();
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      aria-label={evaluator.name}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
      onClick={() => onChange(!checked)}
    >
      <span className="flex size-4 shrink-0 items-center justify-center">
        {checked ? <Check className="size-3.5" aria-hidden /> : null}
      </span>
      <span className="min-w-0 truncate">{evaluatorOptionLabel(evaluator, msg)}</span>
    </button>
  );
}

function EvaluatorChip({ label, removeLabel, onRemove }: { label: string; removeLabel: string; onRemove: () => void }) {
  return (
    <span className="inline-flex max-w-full items-center gap-1 rounded-md bg-sky-50 px-2 py-0.5 text-xs text-sky-700 dark:bg-sky-950 dark:text-sky-200">
      <span className="truncate">{label}</span>
      <button
        type="button"
        className="text-sky-700/70 hover:text-sky-900 dark:text-sky-200"
        aria-label={removeLabel}
        onClick={onRemove}
      >
        <X className="size-3" aria-hidden />
      </button>
    </span>
  );
}

function StepCard({
  index,
  title,
  hint,
  children,
}: {
  index: string;
  title: string;
  hint: string;
  children: ReactNode;
}) {
  return (
    <section className="grid gap-4 rounded-lg border px-5 py-5">
      <div>
        <h2 className="flex items-center gap-2 text-sm font-medium">
          <span className="inline-flex size-5 items-center justify-center rounded-full border border-[var(--success)] text-xs text-[var(--success)]">
            {index}
          </span>
          {title}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">{hint}</p>
      </div>
      {children}
    </section>
  );
}

function OptionalEditor({
  title,
  hint,
  addLabel,
  open,
  onOpen,
  empty,
  children,
}: {
  title: string;
  hint: string;
  addLabel: string;
  open: boolean;
  onOpen: () => void;
  empty?: string;
  children: ReactNode;
}) {
  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium">{title}</span>
        {open ? null : (
          <button type="button" className="text-sm text-muted-foreground hover:text-foreground" onClick={onOpen}>
            <span aria-hidden>+ </span>
            {addLabel}
          </button>
        )}
      </div>
      <p className="text-sm text-muted-foreground">{hint}</p>
      {open ? (
        children
      ) : empty ? (
        <p className="rounded-md bg-muted px-3 py-2 text-sm text-muted-foreground">{empty}</p>
      ) : null}
    </div>
  );
}

function RequiredLabel({ htmlFor, children }: { htmlFor: string; children: string }) {
  return (
    <div className="flex items-center gap-0.5">
      <span className="text-destructive" aria-hidden>
        *
      </span>
      <Label htmlFor={htmlFor}>{children}</Label>
    </div>
  );
}

function EnvField({
  value,
  invalid,
  onChange,
}: {
  value: string;
  invalid: boolean;
  onChange: (event: ChangeEvent<HTMLTextAreaElement>) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-2">
      <Label htmlFor="env">{msg('evaluations.env', 'Environment variables')}</Label>
      <Textarea id="env" value={value} onChange={onChange} />
      <p className="text-sm text-muted-foreground">
        {msg('evaluations.envHint', 'One KEY=VALUE per line, or a JSON object of strings.')}
      </p>
      {invalid ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.envInvalid', 'Environment variables must be KEY=VALUE lines or a JSON object of strings.')}
        </p>
      ) : null}
    </div>
  );
}

function ResourcesField({
  value,
  invalid,
  onChange,
}: {
  value: string;
  invalid: boolean;
  onChange: (event: ChangeEvent<HTMLTextAreaElement>) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-2">
      <Label htmlFor="resources">{msg('evaluations.resources', 'Resources')}</Label>
      <Textarea id="resources" value={value} onChange={onChange} />
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.resourcesHint',
          'JSON array of file, repository, secret, or memory mounts, in the same shape a session accepts.',
        )}
      </p>
      {invalid ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.resourcesInvalid', 'Resources must be a JSON array.')}
        </p>
      ) : null}
    </div>
  );
}

function Field({
  label,
  name,
  value,
  onChange,
}: {
  label: string;
  name: string;
  value: string;
  onChange: (event: ChangeEvent<HTMLInputElement>) => void;
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={name}>{label}</Label>
      <Input id={name} value={value} onChange={onChange} />
    </div>
  );
}

function datasetOptions(datasets: EvaluationDataset[]) {
  return datasets
    .filter((dataset) => dataset.status === 'available')
    .map((dataset) => ({ value: dataset.id, label: dataset.name }));
}

function datasetVersionOptions(datasets: EvaluationDataset[], datasetId: string) {
  const source = datasetId ? datasets.filter((dataset) => dataset.id === datasetId) : datasets;
  return source.flatMap((dataset) =>
    dataset.status === 'available' && dataset.version.id
      ? [{ value: dataset.version.id, label: `${dataset.name} v${dataset.version.version}` }]
      : [],
  );
}

type BindableEvaluator = {
  id: string;
  name: string;
  kind: 'agent' | 'llm';
  builtin: boolean;
};

function bindableEvaluators(
  custom: EvaluationEvaluator[] | undefined,
  presets: EvaluationEvaluatorPreset[] | undefined,
) {
  const rows: BindableEvaluator[] = [];
  const seen = new Set<string>();
  for (const item of custom ?? []) {
    pushBindable(rows, seen, item.id, item.name, item.status, item.version?.evaluator_type, false);
  }
  for (const item of presets ?? []) {
    pushBindable(rows, seen, item.id, item.name, item.status, item.version?.evaluator_type, true);
  }
  return rows;
}

function pushBindable(
  rows: BindableEvaluator[],
  seen: Set<string>,
  id: string,
  name: string,
  status: string,
  kind: string | undefined,
  builtin: boolean,
) {
  if (status !== 'available' || id.length === 0 || name.length === 0 || seen.has(id)) {
    return;
  }
  seen.add(id);
  rows.push({ id, name, kind: kind === 'llm' ? 'llm' : 'agent', builtin });
}

function filterEvaluators(evaluators: BindableEvaluator[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return evaluators;
  }
  return evaluators.filter((item) => item.name.toLowerCase().includes(needle));
}

function evaluatorOptionLabel(evaluator: BindableEvaluator, msg: ReturnType<typeof useI18n>['msg']) {
  const kind =
    evaluator.kind === 'llm'
      ? msg('evaluations.evaluatorKindLlm', 'LLM')
      : msg('evaluations.evaluatorKindAgent', 'Agent');
  const origin = evaluator.builtin
    ? msg('evaluations.presetOrigin', 'Preset')
    : msg('evaluations.customBadge', 'Custom');
  return `${evaluator.name} · ${kind} · ${origin}`;
}

function evaluatorListError(custom: unknown, preset: unknown) {
  if (custom) {
    return evaluationErrorMessage(custom);
  }
  if (preset) {
    return evaluationErrorMessage(preset);
  }
  return '';
}

function toggleEvaluator(current: string[], id: string, checked: boolean) {
  if (!checked) {
    return current.filter((item) => item !== id);
  }
  return current.includes(id) ? current : [...current, id];
}
