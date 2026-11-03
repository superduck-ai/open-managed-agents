import { useEffect, useRef, useState, type ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import {
  evaluationErrorMessage,
  type EvaluationDataset,
  type EvaluationDatasetPreset,
  type EvaluationExperiment,
  type EvaluationExperimentScore,
} from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { NativeIdSelect, useNamedRecords } from './EvaluationPickers';
import { ExperimentModeNav } from './ExperimentsPage';
import { nameMap } from './experimentSummary';
import { useEvaluationDatasetList, useEvaluationDatasetPresetList } from './useEvaluationDatasets';
import { useEvaluationEvaluatorList, useEvaluationEvaluatorPresetList } from './useEvaluationEvaluators';
import {
  useCompareEvaluationExperiments,
  useEvaluationExperimentBaseline,
  useEvaluationExperimentList,
} from './useEvaluationExperiments';

const comparePickLimit = 3;
const agentsPath = '/v1/agents?beta=true&limit=50';

export function ExperimentComparePage() {
  const { msg } = useI18n();
  const { query } = useEvaluationExperimentList();
  const compare = useCompareEvaluationExperiments();
  const datasets = useEvaluationDatasetList();
  const datasetPresets = useEvaluationDatasetPresetList();
  const evaluators = useEvaluationEvaluatorList();
  const evaluatorPresets = useEvaluationEvaluatorPresetList();
  const agents = useNamedRecords(agentsPath);
  const [baselineId, setBaselineId] = useState('');
  const [agentId, setAgentId] = useState('');
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [submitted, setSubmitted] = useState(false);
  const requested = useRef('');
  const completed = (query.data?.data ?? []).filter((experiment) => experiment.status === 'completed');
  const baseline = completed.find((experiment) => experiment.id === baselineId);
  const listedIds = evaluatorIds(baseline);
  const detail = useEvaluationExperimentBaseline(baselineId, Boolean(baseline) && listedIds.length === 0);
  const ids = listedIds.length > 0 ? listedIds : evaluatorIds(detail.data);
  const idKey = ids.join('|');
  const controls = baseline && ids.length > 0 ? { versionId: baseline.version_id, ids } : undefined;
  const pool = (compare.data?.data ?? []).filter((experiment) =>
    controls ? sameControls(experiment, controls.versionId, controls.ids) : false,
  );
  const candidates = pool.filter((experiment) => experiment.id !== baselineId);
  const visibleCandidates = candidates.filter(
    (experiment) => agentId === '' || experiment.agent_id === agentId,
  );
  const addable = visibleCandidates.filter((experiment) => !selectedIds.includes(experiment.id));
  const chosen = chosenExperiments(baseline, selectedIds, pool, completed);
  const evaluatorNames = nameMap(evaluators.query.data);
  for (const [id, name] of nameMap(evaluatorPresets.query.data)) {
    if (!evaluatorNames.has(id)) {
      evaluatorNames.set(id, name);
    }
  }
  const listReady = !query.isLoading && !query.isError;
  const compareRef = useRef(compare);
  compareRef.current = compare;

  useEffect(() => {
    if (!baseline?.version_id || idKey.length === 0) {
      return;
    }
    const key = `${baseline.id}:${baseline.version_id}:${idKey}`;
    if (requested.current === key) {
      return;
    }
    requested.current = key;
    setSelectedIds([]);
    setSubmitted(false);
    setAgentId('');
    compareRef.current.mutate({ dataset_version_id: baseline.version_id, evaluator_ids: idKey.split('|') });
  }, [baseline?.id, baseline?.version_id, idKey]);

  function chooseBaseline(next: string) {
    setBaselineId(next);
    if (next === '') {
      requested.current = '';
      setSelectedIds([]);
      setSubmitted(false);
      setAgentId('');
    }
  }

  function addComparison(next: string) {
    if (next === '' || selectedIds.includes(next) || selectedIds.length >= comparePickLimit) {
      return;
    }
    setSelectedIds((current) => [...current, next]);
    setSubmitted(false);
  }

  const subject = baseline
    ? subjectLabel(baseline, datasets.query.data?.data ?? [], datasetPresets.query.data?.data ?? [], agents.data ?? [])
    : '';
  const datasetLabel = baseline
    ? datasetFixedLabel(baseline, datasets.query.data?.data ?? [], datasetPresets.query.data?.data ?? [])
    : '';

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <ResourcePageHeader
        title={msg('evaluations.experiments', 'Experiments')}
        description={msg(
          'evaluations.experimentsDescription',
          'Run experiments and score how an agent does on a dataset.',
        )}
      />
      <ExperimentModeNav current="compare" />
      {query.error ? <p className="mb-4 text-sm text-destructive">{evaluationErrorMessage(query.error)}</p> : null}
      <section className="rounded-lg border px-5 py-5">
        <h2 className="text-sm font-medium">{msg('evaluations.chooseBaseline', 'Choose a baseline')}</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {msg(
            'evaluations.compareNote',
            'The dataset version and evaluators stay fixed. Only the agent or agent version changes.',
          )}
        </p>
        <div className="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1.4fr)_11rem]">
          <NativeIdSelect
            id="baseline"
            bare
            label={msg('evaluations.baseline', 'Baseline')}
            placeholder={msg('evaluations.selectBaseline', 'Select a completed experiment')}
            value={baselineId}
            options={completed.map((experiment) => ({ value: experiment.id, label: experiment.name }))}
            onChange={chooseBaseline}
          />
          <select
            aria-label={msg('evaluations.compareSubject', 'Baseline agent and dataset')}
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm disabled:text-foreground"
            value={baseline ? baseline.id : ''}
            disabled
          >
            <option value="">{baseline ? subject : ''}</option>
            {baseline ? <option value={baseline.id}>{subject}</option> : null}
          </select>
          <NativeIdSelect
            id="agent_filter"
            bare
            label={msg('evaluations.allAgents', 'All agents')}
            placeholder={msg('evaluations.allAgents', 'All agents')}
            value={agentId}
            options={agentOptions(candidates, agents.data ?? [])}
            onChange={setAgentId}
          />
        </div>
        {baseline && ids.length > 0 ? (
          <div className="mt-4 rounded-lg bg-muted/40 px-4 py-4">
            <h3 className="text-sm font-medium">{msg('evaluations.compareFixed', 'Fixed for this comparison')}</h3>
            <div className="mt-3 grid gap-3">
              <FixedRow label={msg('evaluations.datasetField', 'Dataset')}>
                <p className="rounded-md border bg-background px-3 py-2 text-sm">{datasetLabel}</p>
              </FixedRow>
              <FixedRow label={msg('evaluations.evaluators', 'Evaluators')}>
                <div className="flex min-h-9 flex-wrap items-center gap-2 rounded-md border bg-background px-3 py-2">
                  {ids.map((id) => (
                    <span
                      key={id}
                      className="inline-flex max-w-full items-center rounded-md bg-sky-50 px-2 py-0.5 text-xs text-sky-700 dark:bg-sky-950 dark:text-sky-200"
                    >
                      {evaluatorNames.get(id) || id}
                    </span>
                  ))}
                </div>
              </FixedRow>
            </div>
          </div>
        ) : null}
        {detail.error ? <p className="mt-3 text-sm text-destructive">{evaluationErrorMessage(detail.error)}</p> : null}
        {compare.error ? <p className="mt-3 text-sm text-destructive">{evaluationErrorMessage(compare.error)}</p> : null}
        <div className="mt-6">
          <h2 className="text-sm font-medium">{msg('evaluations.chooseComparisons', 'Choose experiments to compare')}</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {msg(
              'evaluations.comparePickNote',
              'Only experiments with the same dataset and evaluators are listed. Choose up to {count} more.',
              { count: comparePickLimit },
            )}
          </p>
          <select
            aria-label={msg('evaluations.chooseComparisons', 'Choose experiments to compare')}
            className="mt-4 h-9 w-full rounded-md border border-input bg-background px-3 text-center text-sm text-muted-foreground"
            value=""
            disabled={addable.length === 0 || selectedIds.length >= comparePickLimit}
            onChange={(event) => addComparison(event.target.value)}
          >
            <option value="">
              {addable.length === 0
                ? msg('evaluations.compareNoneMatch', 'No experiments match these controls')
                : msg('evaluations.chooseComparisons', 'Choose experiments to compare')}
            </option>
            {addable.map((experiment) => (
              <option key={experiment.id} value={experiment.id}>
                {experiment.name}
              </option>
            ))}
          </select>
          {selectedIds.length > 0 ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {selectedIds.map((id) => {
                const experiment = pool.find((item) => item.id === id) ?? completed.find((item) => item.id === id);
                const name = experiment?.name || id;
                return (
                  <button
                    key={id}
                    type="button"
                    className="inline-flex max-w-full items-center gap-1 rounded-md bg-sky-50 px-2 py-0.5 text-xs text-sky-700 dark:bg-sky-950 dark:text-sky-200"
                    aria-label={msg('evaluations.removeComparison', 'Remove {name}', { name })}
                    onClick={() => {
                      setSelectedIds((current) => current.filter((item) => item !== id));
                      setSubmitted(false);
                    }}
                  >
                    {name}
                    <span aria-hidden>×</span>
                  </button>
                );
              })}
            </div>
          ) : null}
          <div className="mt-3 flex items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">
              {addable.length === 0 ? msg('evaluations.compareNoMore', 'No more experiments match') : ''}
            </p>
            <Button
              type="button"
              className="h-8"
              disabled={selectedIds.length === 0 || compare.isPending}
              onClick={() => setSubmitted(true)}
            >
              {selectedIds.length === 0
                ? msg('evaluations.compareChooseFirst', 'Choose experiments to compare')
                : msg('evaluations.compareAction', 'Compare')}
            </Button>
          </div>
        </div>
        {listReady && completed.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {msg('evaluations.compareEmpty', 'No completed experiments.')}
          </p>
        ) : null}
      </section>
      {submitted ? <CompareResults experiments={chosen} /> : null}
    </section>
  );
}

function FixedRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid items-center gap-3 sm:grid-cols-[5rem_minmax(0,1fr)]">
      <p className="text-sm text-muted-foreground">{label}</p>
      {children}
    </div>
  );
}

function CompareResults({ experiments }: { experiments: EvaluationExperiment[] }) {
  const { msg } = useI18n();
  if (experiments.length === 0) {
    return null;
  }
  const evaluatorIDs = compareEvaluatorIDs(experiments);
  return (
    <div className="mt-4 overflow-x-auto rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
            <TableHead>{msg('evaluations.agentId', 'Agent')}</TableHead>
            <TableHead>{msg('evaluations.agentVersion', 'Agent version')}</TableHead>
            <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
            {evaluatorIDs.map((evaluatorID) => (
              <TableHead key={evaluatorID}>{evaluatorID}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {experiments.map((experiment) => (
            <TableRow key={experiment.id}>
              <TableCell>{experiment.name}</TableCell>
              <TableCell>{experiment.agent_id}</TableCell>
              <TableCell>{experiment.agent_version}</TableCell>
              <TableCell>{experiment.status}</TableCell>
              {evaluatorIDs.map((evaluatorID) => (
                <TableCell key={evaluatorID}>{compareScoreText(experiment, evaluatorID, msg)}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

type CompareMessage = (key: string, fallback: string, values?: Record<string, string | number>) => string;

function chosenExperiments(
  baseline: EvaluationExperiment | undefined,
  selectedIds: string[],
  pool: EvaluationExperiment[],
  completed: EvaluationExperiment[],
) {
  if (!baseline) {
    return [];
  }
  return [baseline.id, ...selectedIds].flatMap((id) => {
    const experiment = pool.find((item) => item.id === id) ?? completed.find((item) => item.id === id);
    return experiment ? [experiment] : [];
  });
}

function subjectLabel(
  experiment: EvaluationExperiment,
  datasets: EvaluationDataset[],
  presets: EvaluationDatasetPreset[],
  agents: Array<{ id: string; name: string }>,
) {
  const parts = datasetParts(experiment, datasets, presets);
  const agent = agents.find((item) => item.id === experiment.agent_id)?.name || experiment.agent_id;
  return `${agent} - ${parts.name}${parts.version ? ` / v${parts.version}` : ''}`;
}

function datasetFixedLabel(
  experiment: EvaluationExperiment,
  datasets: EvaluationDataset[],
  presets: EvaluationDatasetPreset[],
) {
  const parts = datasetParts(experiment, datasets, presets);
  return parts.version ? `${parts.name} · v${parts.version}` : parts.name;
}

function datasetParts(
  experiment: EvaluationExperiment,
  datasets: EvaluationDataset[],
  presets: EvaluationDatasetPreset[],
) {
  const dataset = datasets.find((item) => item.id === experiment.dataset_id);
  if (dataset) {
    return {
      name: dataset.name,
      version: dataset.version?.id === experiment.version_id ? dataset.version.version : undefined,
    };
  }
  const preset = presets.find((item) => item.id === experiment.dataset_id);
  const version = preset?.versions.find((item) => item.id === experiment.version_id);
  return { name: preset?.name || experiment.dataset_id, version: version?.version };
}

function agentOptions(experiments: EvaluationExperiment[], agents: Array<{ id: string; name: string }>) {
  const ids: string[] = [];
  for (const experiment of experiments) {
    if (experiment.agent_id && !ids.includes(experiment.agent_id)) {
      ids.push(experiment.agent_id);
    }
  }
  return ids.map((id) => ({ value: id, label: agents.find((agent) => agent.id === id)?.name || id }));
}

function sameControls(experiment: EvaluationExperiment, versionId: string, evaluatorIDs: string[]) {
  if (experiment.version_id !== versionId) {
    return false;
  }
  const ids = evaluatorIds(experiment);
  return ids.length === evaluatorIDs.length && evaluatorIDs.every((id) => ids.includes(id));
}

function evaluatorIds(experiment: EvaluationExperiment | undefined) {
  return (experiment?.evaluators ?? []).map((evaluator) => evaluator.id);
}

function compareEvaluatorIDs(experiments: EvaluationExperiment[]) {
  const ordered: string[] = [];
  const seen = new Set<string>();
  const evaluators = experiments.flatMap((experiment) => experiment.evaluators ?? []);
  evaluators.sort((left, right) => left.position - right.position);
  for (const evaluator of evaluators) {
    if (seen.has(evaluator.id)) {
      continue;
    }
    seen.add(evaluator.id);
    ordered.push(evaluator.id);
  }
  for (const experiment of experiments) {
    for (const score of experiment.scores ?? []) {
      if (seen.has(score.evaluator_id)) {
        continue;
      }
      seen.add(score.evaluator_id);
      ordered.push(score.evaluator_id);
    }
  }
  return ordered;
}

function compareScoreText(experiment: EvaluationExperiment, evaluatorID: string, msg: CompareMessage) {
  const scores = (experiment.scores ?? []).filter((score) => score.evaluator_id === evaluatorID);
  if (scores.length === 0) {
    return msg('evaluations.scoreMissing', 'Missing');
  }
  return scores.map((score) => compareScoreValue(score, msg)).join(', ');
}

function compareScoreValue(score: EvaluationExperimentScore, msg: CompareMessage) {
  if (score.status === 'failed') {
    return score.error || msg('evaluations.scoreFailed', 'Failed');
  }
  if (score.status === 'completed' && typeof score.score === 'number') {
    return String(score.score);
  }
  return msg('evaluations.scoreMissing', 'Missing');
}
