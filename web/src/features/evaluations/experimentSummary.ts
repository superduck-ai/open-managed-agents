import type {
  EvaluationCase,
  EvaluationDataset,
  EvaluationExperiment,
  EvaluationExperimentItem,
  EvaluationExperimentScore,
  EvaluationExperimentStatus,
} from './api';

export type ExperimentScope = 'all' | 'passed';

export type ExperimentStep = {
  completed: number;
  failed: number;
  total: number;
  status: EvaluationExperimentStatus;
};

export type ExperimentNames = {
  agent: string;
  dataset: string;
  evaluators: Map<string, string>;
  cases: Map<string, string>;
};

export type EvaluatorScoreSummary = {
  evaluatorId: string;
  passed: number;
  failed: number;
  average?: number;
  low?: number;
  high?: number;
};

export type ExperimentCaseRow = {
  id: string;
  caseId: string;
  sessionId: string;
  status: string;
  error: string;
  scores: EvaluationExperimentScore[];
};

export type RunMetrics = {
  targetCases: string;
  callSuccess: string;
  averageScore: string;
  scoresDone: string;
  passedCases: string;
  failedCases: string;
  evaluators: string;
  sessions: string;
};

export function datasetRecord(value: unknown): EvaluationDataset | undefined {
  if (!value || typeof value !== 'object') {
    return undefined;
  }
  const record = value as Partial<EvaluationDataset>;
  if (record.type !== 'evaluation_dataset' || typeof record.name !== 'string') {
    return undefined;
  }
  return record as EvaluationDataset;
}

export function nameMap(value: unknown) {
  const map = new Map<string, string>();
  if (!value || typeof value !== 'object' || !('data' in value) || !Array.isArray(value.data)) {
    return map;
  }
  for (const item of value.data) {
    const named = namedRecord(item);
    if (named) {
      map.set(named.id, named.name);
    }
  }
  return map;
}

export function caseRecords(value: unknown) {
  if (!value || typeof value !== 'object' || !('data' in value) || !Array.isArray(value.data)) {
    return [];
  }
  return value.data.filter(isEvaluationCase);
}

export function experimentNames(input: {
  experiment: EvaluationExperiment;
  dataset: EvaluationDataset | undefined;
  evaluators: Map<string, string>;
  cases: EvaluationCase[];
  agents: Array<{ id: string; name: string }>;
}): ExperimentNames {
  const agent = input.agents.find((item) => item.id === input.experiment.agent_id)?.name || input.experiment.agent_id;
  return {
    agent,
    dataset: datasetLine(input.experiment, input.dataset),
    evaluators: input.evaluators,
    cases: caseNameMap(input.cases, input.experiment.version_id),
  };
}

export function callStep(experiment: EvaluationExperiment): ExperimentStep {
  const items = experiment.items ?? [];
  if (items.length > 0) {
    return itemStep(items);
  }
  return countStep(experiment.item_count, experiment.session_count, experiment.status);
}

export function scoreStep(experiment: EvaluationExperiment): ExperimentStep {
  const scores = experiment.scores ?? [];
  const total = expectedScoreCount(experiment);
  const completed = scores.filter((score) => score.status === 'completed').length;
  const failed = scores.filter((score) => score.status === 'failed').length;
  const settled =
    experiment.status === 'completed' || experiment.status === 'failed' || experiment.status === 'cancelled';
  const started = completed + failed > 0 || experiment.status === 'running';
  return {
    completed,
    failed,
    total,
    status: stepStatus(total, completed, failed, !settled && started && completed + failed < total),
  };
}

export function reportStep(score: ExperimentStep, experiment: EvaluationExperiment): ExperimentStep {
  if (score.status === 'completed' || score.status === 'failed') {
    return { completed: score.completed, failed: score.failed, total: score.total, status: 'completed' };
  }
  if (score.completed > 0 || score.failed > 0 || experiment.status === 'running') {
    return { completed: score.completed, failed: score.failed, total: score.total, status: 'running' };
  }
  return { completed: 0, failed: 0, total: score.total, status: 'pending' };
}

export function stepCountLabel(step: ExperimentStep) {
  if (step.total <= 0) {
    return '';
  }
  const done = step.completed + step.failed;
  return `${done} / ${step.total} (${step.completed} ok)`;
}

export function runMetrics(experiment: EvaluationExperiment, scope: ExperimentScope): RunMetrics {
  const items = experiment.items ?? [];
  const scores = scopedScores(experiment, scope);
  const target = metricTarget(experiment, items, scope);
  const passedItems = items.filter((item) => item.status === 'completed').length;
  const failedItems = items.filter((item) => item.status === 'failed' || item.status === 'cancelled').length;
  const values = scoreValues(scores);
  return {
    targetCases: String(target.count),
    callSuccess: target.known ? formatPercent(target.passed, target.count) : '—',
    averageScore: formatScore(average(values)) || '—',
    scoresDone: scoreProgress(experiment, scores),
    passedCases: items.length > 0 ? String(scope === 'passed' ? target.count : passedItems) : '—',
    failedCases: items.length > 0 && scope === 'all' ? String(failedItems) : items.length > 0 ? '0' : '—',
    evaluators: String(experiment.evaluator_count),
    sessions: sessionMetric(experiment, items, scope),
  };
}

export function evaluatorOrder(experiment: EvaluationExperiment) {
  const bound = [...(experiment.evaluators ?? [])]
    .sort((left, right) => left.position - right.position)
    .map((item) => item.id);
  const extra = [...new Set((experiment.scores ?? []).map((score) => score.evaluator_id))].filter(
    (id) => !bound.includes(id),
  );
  return [...bound, ...extra];
}

export function evaluatorSummaries(experiment: EvaluationExperiment) {
  return evaluatorOrder(experiment).map((evaluatorId) => summarizeEvaluator(experiment, evaluatorId));
}

export function caseRows(experiment: EvaluationExperiment) {
  const rows = (experiment.items ?? []).map((item) => rowFromItem(experiment, item));
  const seen = new Set(rows.map((row) => row.id));
  for (const score of experiment.scores ?? []) {
    if (seen.has(score.item_id)) {
      continue;
    }
    seen.add(score.item_id);
    rows.push(rowFromScore(experiment, score.item_id));
  }
  return rows;
}

export function visibleCaseRows(rows: ExperimentCaseRow[], query: string, status: string, names: Map<string, string>) {
  const needle = query.trim().toLowerCase();
  return rows.filter((row) => {
    if (status && row.status !== status) {
      return false;
    }
    if (!needle) {
      return true;
    }
    const parts = [row.caseId, row.sessionId, row.id, names.get(row.caseId) ?? ''];
    return parts.some((part) => part.toLowerCase().includes(needle));
  });
}

export function formatScore(value: number | undefined) {
  if (typeof value !== 'number' || Number.isNaN(value)) {
    return '';
  }
  return String(Math.round(value * 100) / 100);
}

export function scorePercentLabel(value: number | undefined) {
  if (typeof value !== 'number' || Number.isNaN(value)) {
    return '';
  }
  if (value < 0 || value > 1) {
    return formatScore(value);
  }
  return `${Math.round(value * 1000) / 10}%`;
}

export function experimentDateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export type DetailFact =
  | {
      kind: 'round';
      index?: number;
      model?: string;
      seconds?: number;
      inputTokens?: number;
      outputTokens?: number;
    }
  | { kind: 'field'; key: string; value: string | number | boolean };

export type ReportIssue = {
  evaluatorId: string;
  affected: number;
  failed: number;
  partial: number;
  notes: Array<{ id: string; itemId: string; text: string }>;
};

export function reportIssues(experiment: EvaluationExperiment): ReportIssue[] {
  return evaluatorSummaries(experiment).flatMap((summary) => {
    const scores = (experiment.scores ?? []).filter((score) => score.evaluator_id === summary.evaluatorId);
    const notes = scores.flatMap((score) => {
      const partial = score.status === 'completed' && typeof score.score === 'number' && score.score < 1;
      if (score.status !== 'failed' && !partial) {
        return [];
      }
      const text = (score.status === 'failed' ? score.error || score.explanation : score.explanation || score.error) ?? '';
      return [{ id: score.id, itemId: score.item_id, text: text.trim() }];
    });
    if (notes.length === 0) {
      return [];
    }
    const failed = scores.filter((score) => score.status === 'failed' && notes.some((note) => note.id === score.id)).length;
    return [
      {
        evaluatorId: summary.evaluatorId,
        affected: new Set(notes.map((note) => note.itemId)).size,
        failed,
        partial: notes.length - failed,
        notes,
      },
    ];
  });
}

export function detailFacts(details: unknown): DetailFact[] {
  if (!isDetailRecord(details)) {
    return [];
  }
  const facts: DetailFact[] = [];
  if (Array.isArray(details.rounds)) {
    for (const round of details.rounds) {
      const fact = roundFact(round);
      if (fact) {
        facts.push(fact);
      }
    }
  }
  const covered = new Set(Array.isArray(details.rounds) ? ['rounds', 'slowest_seconds'] : ['rounds']);
  for (const [key, value] of Object.entries(details)) {
    if (covered.has(key) || value === '' || !isDetailPrimitive(value)) {
      continue;
    }
    facts.push({ kind: 'field', key, value });
  }
  return facts;
}

export function formatSeconds(value: number) {
  if (Number.isInteger(value)) {
    return String(value);
  }
  return String(Math.round(value * 10) / 10);
}

function roundFact(round: unknown): DetailFact | undefined {
  if (!isDetailRecord(round)) {
    return undefined;
  }
  const fact: Extract<DetailFact, { kind: 'round' }> = { kind: 'round' };
  if (typeof round.index === 'number' && !Number.isNaN(round.index)) {
    fact.index = round.index;
  }
  if (typeof round.model === 'string' && round.model.trim()) {
    fact.model = round.model.trim();
  }
  if (typeof round.duration_seconds === 'number' && !Number.isNaN(round.duration_seconds)) {
    fact.seconds = round.duration_seconds;
  }
  if (typeof round.input_tokens === 'number' && !Number.isNaN(round.input_tokens)) {
    fact.inputTokens = round.input_tokens;
  }
  if (typeof round.output_tokens === 'number' && !Number.isNaN(round.output_tokens)) {
    fact.outputTokens = round.output_tokens;
  }
  if (
    fact.index === undefined &&
    !fact.model &&
    fact.seconds === undefined &&
    fact.inputTokens === undefined &&
    fact.outputTokens === undefined
  ) {
    return undefined;
  }
  return fact;
}

function isDetailRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function isDetailPrimitive(value: unknown): value is string | number | boolean {
  return typeof value === 'string' || (typeof value === 'number' && !Number.isNaN(value)) || typeof value === 'boolean';
}

export function displayName(names: Map<string, string>, id: string) {
  return names.get(id) || id;
}

function namedRecord(value: unknown) {
  if (
    !value ||
    typeof value !== 'object' ||
    !('id' in value) ||
    typeof value.id !== 'string' ||
    value.id.length === 0
  ) {
    return undefined;
  }
  const name = 'name' in value && typeof value.name === 'string' && value.name.length > 0 ? value.name : value.id;
  return { id: value.id, name };
}

function isEvaluationCase(value: unknown): value is EvaluationCase {
  return (
    Boolean(value) && typeof value === 'object' && 'id' in value && typeof value.id === 'string' && 'name' in value
  );
}

function datasetLine(experiment: EvaluationExperiment, dataset: EvaluationDataset | undefined) {
  const name = dataset?.name || experiment.dataset_id;
  if (dataset?.version?.id === experiment.version_id) {
    return `${name} · v${dataset.version.version}`;
  }
  return name;
}

function caseNameMap(cases: EvaluationCase[], versionId: string) {
  const matched = new Map<string, string>();
  const any = new Map<string, string>();
  for (const item of cases) {
    if (!item.name) {
      continue;
    }
    any.set(item.id, item.name);
    if (!versionId || item.version_id === versionId) {
      matched.set(item.id, item.name);
    }
  }
  return matched.size > 0 ? matched : any;
}

function itemStep(items: EvaluationExperimentItem[]): ExperimentStep {
  const completed = items.filter((item) => item.status === 'completed').length;
  const failed = items.filter((item) => item.status === 'failed' || item.status === 'cancelled').length;
  const started = completed + failed > 0;
  const open = items.some((item) => item.status === 'running' || (started && item.status === 'pending'));
  return { completed, failed, total: items.length, status: stepStatus(items.length, completed, failed, open) };
}

function countStep(total: number, started: number, status: EvaluationExperimentStatus): ExperimentStep {
  if (status === 'completed') {
    return { completed: total, failed: 0, total, status: 'completed' };
  }
  if (status === 'failed' || status === 'cancelled') {
    return { completed: Math.min(started, total), failed: 0, total, status: 'failed' };
  }
  if (status === 'running') {
    return { completed: Math.min(started, total), failed: 0, total, status: 'running' };
  }
  return { completed: 0, failed: 0, total, status: 'pending' };
}

function stepStatus(total: number, completed: number, failed: number, open: boolean): EvaluationExperimentStatus {
  if (total === 0 || (completed === 0 && failed === 0 && !open)) {
    return 'pending';
  }
  if (open) {
    return 'running';
  }
  if (failed > 0) {
    return 'failed';
  }
  if (completed >= total) {
    return 'completed';
  }
  return completed > 0 ? 'running' : 'pending';
}

function expectedScoreCount(experiment: EvaluationExperiment) {
  const items = Math.max(experiment.items?.length ?? 0, experiment.item_count);
  const evaluators = Math.max(
    experiment.evaluators?.length ?? 0,
    experiment.evaluator_count,
    evaluatorOrder(experiment).length,
  );
  if (items === 0 || evaluators === 0) {
    return (experiment.scores ?? []).length;
  }
  return items * evaluators;
}

function scopedScores(experiment: EvaluationExperiment, scope: ExperimentScope) {
  const scores = experiment.scores ?? [];
  const items = experiment.items ?? [];
  if (scope === 'all' || items.length === 0) {
    return scope === 'passed' && items.length === 0 ? [] : scores;
  }
  const passed = new Set(items.filter((item) => item.status === 'completed').map((item) => item.id));
  return scores.filter((score) => passed.has(score.item_id));
}

function metricTarget(experiment: EvaluationExperiment, items: EvaluationExperimentItem[], scope: ExperimentScope) {
  if (items.length === 0) {
    return {
      count: scope === 'passed' ? 0 : experiment.item_count,
      passed: 0,
      known: false,
    };
  }
  const passed = items.filter((item) => item.status === 'completed').length;
  if (scope === 'passed') {
    return { count: passed, passed, known: true };
  }
  return { count: items.length, passed, known: true };
}

function scoreProgress(experiment: EvaluationExperiment, scores: EvaluationExperimentScore[]) {
  const total = expectedScoreCount(experiment);
  if (total <= 0) {
    return '—';
  }
  const done = scores.filter((score) => score.status === 'completed' || score.status === 'failed').length;
  return `${done}/${total}`;
}

function sessionMetric(experiment: EvaluationExperiment, items: EvaluationExperimentItem[], scope: ExperimentScope) {
  if (items.length === 0) {
    return scope === 'passed' ? '0' : String(experiment.session_count);
  }
  const source = scope === 'passed' ? items.filter((item) => item.status === 'completed') : items;
  return String(source.filter((item) => item.session_id).length);
}

function scoreValues(scores: EvaluationExperimentScore[]) {
  return scores.flatMap((score) =>
    score.status === 'completed' && typeof score.score === 'number' ? [score.score] : [],
  );
}

function average(values: number[]) {
  if (values.length === 0) {
    return undefined;
  }
  return values.reduce((sum, value) => sum + value, 0) / values.length;
}

function summarizeEvaluator(experiment: EvaluationExperiment, evaluatorId: string): EvaluatorScoreSummary {
  const scores = (experiment.scores ?? []).filter((score) => score.evaluator_id === evaluatorId);
  const values = scoreValues(scores);
  return {
    evaluatorId,
    passed: scores.filter((score) => score.status === 'completed').length,
    failed: scores.filter((score) => score.status === 'failed').length,
    average: average(values),
    low: values.length > 0 ? Math.min(...values) : undefined,
    high: values.length > 0 ? Math.max(...values) : undefined,
  };
}

function rowFromItem(experiment: EvaluationExperiment, item: EvaluationExperimentItem): ExperimentCaseRow {
  return {
    id: item.id,
    caseId: item.case_id,
    sessionId: item.session_id ?? '',
    status: item.status,
    error: item.error ?? '',
    scores: scoresForItem(experiment, item.id),
  };
}

function rowFromScore(experiment: EvaluationExperiment, itemId: string): ExperimentCaseRow {
  const scores = scoresForItem(experiment, itemId);
  const failed = scores.some((score) => score.status === 'failed');
  return {
    id: itemId,
    caseId: itemId,
    sessionId: '',
    status: failed ? 'failed' : 'completed',
    error: scores.find((score) => score.error)?.error ?? '',
    scores,
  };
}

function scoresForItem(experiment: EvaluationExperiment, itemId: string) {
  return (experiment.scores ?? []).filter((score) => score.item_id === itemId);
}

function formatPercent(part: number, total: number) {
  if (total <= 0) {
    return '—';
  }
  return `${((part / total) * 100).toFixed(1)}%`;
}
