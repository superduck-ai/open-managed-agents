import { Circle, CircleCheck, CircleX, LoaderCircle, Search } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { Input } from '../../shared/ui/input';
import { TableEmptyRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { type EvaluationCase, type EvaluationExperiment, type EvaluationExperimentScore } from './api';
import { CaseResultDrawer } from './CaseResultDrawer';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import {
  callStep,
  caseRows,
  detailFacts,
  displayName,
  evaluatorSummaries,
  experimentDateLabel,
  formatScore,
  formatSeconds,
  reportIssues,
  reportStep,
  runMetrics,
  scorePercentLabel,
  scoreStep,
  stepCountLabel,
  visibleCaseRows,
  type DetailFact,
  type ExperimentCaseRow,
  type ExperimentNames,
  type ExperimentScope,
  type ExperimentStep,
} from './experimentSummary';

export function ExperimentDetailBody({
  experiment,
  names,
  cases,
  workspaceId,
}: {
  experiment: EvaluationExperiment;
  names: ExperimentNames;
  cases: EvaluationCase[];
  workspaceId: string;
}) {
  return (
    <div className="flex flex-col gap-8">
      <ExperimentFacts experiment={experiment} names={names} />
      <ExperimentSteps experiment={experiment} />
      <ExperimentPanels experiment={experiment} names={names} cases={cases} workspaceId={workspaceId} />
    </div>
  );
}

function ExperimentFacts({ experiment, names }: { experiment: EvaluationExperiment; names: ExperimentNames }) {
  const { msg } = useI18n();
  const fields = [
    { label: msg('evaluations.descriptionField', 'Description'), value: experiment.desc || '—' },
    { label: msg('evaluations.status', 'Status'), value: <EvaluationStatusBadge status={experiment.status} /> },
    { label: msg('evaluations.target', 'Target'), value: names.agent },
    { label: msg('evaluations.datasetField', 'Dataset'), value: names.dataset },
    { label: msg('evaluations.createdAt', 'Created'), value: experimentDateLabel(experiment.created_at) },
    { label: msg('evaluations.updatedAt', 'Updated'), value: experimentDateLabel(experiment.updated_at) },
  ];
  return (
    <section className="rounded-lg border px-5 py-5">
      <dl className="grid gap-x-8 gap-y-4 sm:grid-cols-3">
        {fields.map((field) => (
          <div key={field.label} className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-baseline gap-x-3 text-sm">
            <dt className="text-muted-foreground">{field.label}</dt>
            <dd className="min-w-0 truncate">{field.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function ExperimentSteps({ experiment }: { experiment: EvaluationExperiment }) {
  const { msg } = useI18n();
  const score = scoreStep(experiment);
  const steps = [
    { label: msg('evaluations.callAgent', 'Call Agent'), step: callStep(experiment), count: true },
    { label: msg('evaluations.scoreStep', 'Score'), step: score, count: true },
    { label: msg('evaluations.experimentReport', 'Report'), step: reportStep(score, experiment), count: false },
  ];
  return (
    <div className="flex flex-col gap-3">
      {steps.map((item) => (
        <div key={item.label} className="flex items-center gap-3 rounded-lg border px-4 py-3">
          <StepIcon status={item.step.status} />
          <span className="text-sm font-medium">{item.label}</span>
          <EvaluationStatusBadge status={item.step.status} />
          {item.count && stepCountLabel(item.step) ? (
            <span className="text-sm tabular-nums text-muted-foreground">{stepCountLabel(item.step)}</span>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function StepIcon({ status }: { status: ExperimentStep['status'] }) {
  if (status === 'completed') {
    return <CircleCheck className="size-4 text-[var(--success)]" />;
  }
  if (status === 'failed' || status === 'cancelled') {
    return <CircleX className="size-4 text-destructive" />;
  }
  if (status === 'running') {
    return <LoaderCircle className="size-4 animate-spin text-[var(--warning)]" />;
  }
  return <Circle className="size-4 text-muted-foreground" />;
}

function ExperimentPanels({
  experiment,
  names,
  cases,
  workspaceId,
}: {
  experiment: EvaluationExperiment;
  names: ExperimentNames;
  cases: EvaluationCase[];
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const [tab, setTab] = useState('overview');
  const [openId, setOpenId] = useState('');
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => {
        if (value) {
          setTab(String(value));
        }
      }}
    >
      <TabsList>
        <TabsTrigger value="overview">{msg('evaluations.experimentOverview', 'Overview')}</TabsTrigger>
        <TabsTrigger value="report">{msg('evaluations.experimentReport', 'Report')}</TabsTrigger>
        <TabsTrigger value="cases">{msg('evaluations.experimentCases', 'Case results')}</TabsTrigger>
        <TabsTrigger value="config">{msg('evaluations.experimentConfig', 'Configuration')}</TabsTrigger>
      </TabsList>
      <TabsContent value="overview" className="pt-4">
        <ExperimentOverview experiment={experiment} names={names} />
      </TabsContent>
      <TabsContent value="report" className="pt-4">
        <ExperimentReport
          experiment={experiment}
          names={names}
          onViewCase={(itemId) => {
            setOpenId(itemId);
            setTab('cases');
          }}
        />
      </TabsContent>
      <TabsContent value="cases" className="pt-4">
        <ExperimentCaseResults
          experiment={experiment}
          names={names}
          cases={cases}
          workspaceId={workspaceId}
          openId={openId}
          onOpenId={setOpenId}
        />
      </TabsContent>
      <TabsContent value="config" className="pt-4">
        <ExperimentConfig experiment={experiment} names={names} />
      </TabsContent>
    </Tabs>
  );
}

function ExperimentOverview({ experiment, names }: { experiment: EvaluationExperiment; names: ExperimentNames }) {
  const { msg } = useI18n();
  const [scope, setScope] = useState<ExperimentScope>('all');
  const metrics = runMetrics(experiment, scope);
  const cards = [
    { label: msg('evaluations.targetCases', 'Target cases'), value: metrics.targetCases },
    { label: msg('evaluations.callSuccess', 'Call success'), value: metrics.callSuccess },
    { label: msg('evaluations.averageScore', 'Average'), value: metrics.averageScore },
    { label: msg('evaluations.scoresDone', 'Scores done'), value: metrics.scoresDone },
    { label: msg('evaluations.passedCases', 'Passed cases'), value: metrics.passedCases },
    { label: msg('evaluations.failedCases', 'Failed cases'), value: metrics.failedCases },
    { label: msg('evaluations.evaluators', 'Evaluators'), value: metrics.evaluators },
    { label: msg('evaluations.session', 'Session'), value: metrics.sessions },
  ];
  const summaries = evaluatorSummaries(experiment);
  return (
    <div className="flex flex-col gap-8">
      <section>
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-sm font-medium">{msg('evaluations.runMetrics', 'Run metrics')}</h2>
          <div className="inline-flex rounded-lg border p-0.5">
            <ScopeButton pressed={scope === 'all'} onClick={() => setScope('all')}>
              {msg('evaluations.metricAll', 'All')}
            </ScopeButton>
            <ScopeButton pressed={scope === 'passed'} onClick={() => setScope('passed')}>
              {msg('evaluations.metricPassed', 'Passed')}
            </ScopeButton>
          </div>
        </div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {cards.map((card) => (
            <div key={card.label} className="rounded-lg border px-4 py-4">
              <p className="text-sm text-muted-foreground">{card.label}</p>
              <p className="mt-3 text-2xl font-semibold tracking-tight tabular-nums">{card.value}</p>
            </div>
          ))}
        </div>
      </section>
      <section className="rounded-lg border">
        <h2 className="border-b px-4 py-3 text-sm font-medium">{msg('evaluations.scoreStats', 'Score summary')}</h2>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('evaluations.evaluators', 'Evaluators')}</TableHead>
              <TableHead>{msg('evaluations.metricPassed', 'Passed')}</TableHead>
              <TableHead>{msg('evaluations.scoreFailed', 'Failed')}</TableHead>
              <TableHead>{msg('evaluations.averageScore', 'Average')}</TableHead>
              <TableHead>{msg('evaluations.scoreLow', 'Lowest')}</TableHead>
              <TableHead>{msg('evaluations.scoreHigh', 'Highest')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {summaries.length === 0 ? (
              <TableEmptyRow colSpan={6}>{msg('evaluations.noScores', 'No scores yet.')}</TableEmptyRow>
            ) : (
              summaries.map((summary) => (
                <TableRow key={summary.evaluatorId}>
                  <TableCell>{displayName(names.evaluators, summary.evaluatorId)}</TableCell>
                  <TableCell className="tabular-nums">{summary.passed}</TableCell>
                  <TableCell className="tabular-nums">{summary.failed}</TableCell>
                  <TableCell className="tabular-nums">{formatScore(summary.average) || '—'}</TableCell>
                  <TableCell className="tabular-nums">{formatScore(summary.low) || '—'}</TableCell>
                  <TableCell className="tabular-nums">{formatScore(summary.high) || '—'}</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </section>
    </div>
  );
}

function ScopeButton({ pressed, onClick, children }: { pressed: boolean; onClick: () => void; children: string }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      className={
        pressed
          ? 'h-7 rounded-md bg-muted px-2.5 text-sm text-foreground'
          : 'h-7 rounded-md px-2.5 text-sm text-muted-foreground hover:text-foreground'
      }
      onClick={onClick}
    >
      {children}
    </button>
  );
}

function ExperimentReport({
  experiment,
  names,
  onViewCase,
}: {
  experiment: EvaluationExperiment;
  names: ExperimentNames;
  onViewCase: (itemId: string) => void;
}) {
  const { msg } = useI18n();
  const summaries = evaluatorSummaries(experiment);
  const issues = reportIssues(experiment);
  if (summaries.length === 0) {
    return <p className="text-sm text-muted-foreground">{msg('evaluations.noScores', 'No scores yet.')}</p>;
  }
  return (
    <div className="flex flex-col gap-8">
      <section>
        <h2 className="mb-3 text-sm font-medium">{msg('evaluations.reportIssues', 'Main issues')}</h2>
        {issues.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {msg('evaluations.reportNoIssues', 'No failed scores, and none below a full mark.')}
          </p>
        ) : (
          <div className="flex flex-col gap-3">
            {issues.map((issue) => (
              <article key={issue.evaluatorId} className="rounded-lg border px-4 py-4">
                <div className="flex items-start justify-between gap-3">
                  <h3 className="text-sm font-medium">{displayName(names.evaluators, issue.evaluatorId)}</h3>
                  <p className="shrink-0 text-xs text-muted-foreground">
                    {msg('evaluations.reportImpact', 'Affects {count} cases', { count: issue.affected })}
                  </p>
                </div>
                <p className="mt-2 text-sm text-muted-foreground">{issueSummary(issue, msg)}</p>
                <div className="mt-3 flex flex-col gap-3">
                  {issue.notes.map((note) => (
                    <div key={note.id}>
                      <p className="text-sm font-medium">{caseLabel(experiment, names, note.itemId)}</p>
                      <p className="mt-1 text-sm whitespace-pre-wrap">{note.text || '—'}</p>
                    </div>
                  ))}
                </div>
              </article>
            ))}
          </div>
        )}
      </section>
      <section>
        <h2 className="mb-3 text-sm font-medium">{msg('evaluations.reportResults', 'Evaluator results')}</h2>
        <div className="flex flex-col gap-4">
          {summaries.map((summary) => {
            const scores = (experiment.scores ?? []).filter((score) => score.evaluator_id === summary.evaluatorId);
            const total = summary.passed + summary.failed;
            const average = scorePercentLabel(summary.average);
            return (
              <section key={summary.evaluatorId} className="rounded-lg border">
                <header className="flex items-start justify-between gap-3 border-b px-4 py-3">
                  <div>
                    <h3 className="text-sm font-medium">{displayName(names.evaluators, summary.evaluatorId)}</h3>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {msg('evaluations.reportValid', 'Valid scores: {done} / {total}', {
                        done: summary.passed,
                        total,
                      })}
                    </p>
                  </div>
                  <p className="text-sm tabular-nums">
                    {average
                      ? msg('evaluations.reportAverage', 'Average {score}', { score: average })
                      : msg('evaluations.reportAverage', 'Average {score}', { score: '—' })}
                  </p>
                </header>
                <div className="flex flex-col gap-3 px-4 py-4">
                  {scores.map((score) => (
                    <ScoreNote
                      key={score.id}
                      score={score}
                      caseLabel={caseLabel(experiment, names, score.item_id)}
                      onViewCase={() => onViewCase(score.item_id)}
                    />
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      </section>
    </div>
  );
}

function issueSummary(
  issue: { failed: number; partial: number },
  msg: ReturnType<typeof useI18n>['msg'],
) {
  const parts: string[] = [];
  if (issue.failed > 0) {
    parts.push(msg('evaluations.reportIssueFailed', '{count} scores failed.', { count: issue.failed }));
  }
  if (issue.partial > 0) {
    parts.push(
      msg('evaluations.reportIssuePartial', '{count} scores are below a full mark.', { count: issue.partial }),
    );
  }
  return parts.join(' ');
}

function ScoreNote({
  score,
  caseLabel: label,
  onViewCase,
}: {
  score: EvaluationExperimentScore;
  caseLabel: string;
  onViewCase: () => void;
}) {
  const { msg } = useI18n();
  const percent = scorePercentLabel(score.score);
  const status =
    score.status === 'failed'
      ? msg('evaluations.scoreFailed', 'Failed')
      : msg('evaluations.metricPassed', 'Passed');
  const statusLine = percent
    ? msg('evaluations.reportStatus', '{status} · {score}', { status, score: percent })
    : status;
  return (
    <article className="rounded-lg bg-muted/40 px-4 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-medium">{label}</p>
          <p className="mt-1 text-xs text-muted-foreground">{statusLine}</p>
        </div>
        <button
          type="button"
          className="shrink-0 text-sm text-muted-foreground hover:text-foreground"
          onClick={onViewCase}
        >
          {msg('evaluations.reportViewCase', 'View case')}
        </button>
      </div>
      <p className="mt-2 text-sm whitespace-pre-wrap">{score.explanation || score.error || '—'}</p>
      <DetailFacts details={score.details} />
    </article>
  );
}

function DetailFacts({ details }: { details: unknown }) {
  const { msg } = useI18n();
  const facts = detailFacts(details);
  if (facts.length === 0) {
    return null;
  }
  return (
    <ul className="mt-2 flex flex-col gap-1 text-sm text-muted-foreground">
      {facts.map((fact, index) => (
        <li key={index}>{formatDetailFact(fact, msg)}</li>
      ))}
    </ul>
  );
}

function formatDetailFact(fact: DetailFact, msg: ReturnType<typeof useI18n>['msg']) {
  if (fact.kind === 'field') {
    return formatDetailField(fact, msg);
  }
  const parts: string[] = [];
  if (fact.index !== undefined) {
    parts.push(msg('evaluations.detailRound', 'Round {index}', { index: fact.index }));
  }
  if (fact.model) {
    parts.push(fact.model);
  }
  if (fact.seconds !== undefined) {
    parts.push(msg('evaluations.detailSeconds', '{seconds}s', { seconds: formatSeconds(fact.seconds) }));
  }
  if (fact.inputTokens !== undefined) {
    parts.push(msg('evaluations.detailInput', 'input {count}', { count: fact.inputTokens }));
  }
  if (fact.outputTokens !== undefined) {
    parts.push(msg('evaluations.detailOutput', 'output {count}', { count: fact.outputTokens }));
  }
  return parts.join(msg('evaluations.detailJoin', ', '));
}

function formatDetailField(
  fact: Extract<DetailFact, { kind: 'field' }>,
  msg: ReturnType<typeof useI18n>['msg'],
) {
  let value: string;
  if (typeof fact.value === 'number' && fact.key.endsWith('_seconds')) {
    value = msg('evaluations.detailSeconds', '{seconds}s', { seconds: formatSeconds(fact.value) });
  } else if (fact.value === true) {
    value = msg('evaluations.detailYes', 'yes');
  } else if (fact.value === false) {
    value = msg('evaluations.detailNo', 'no');
  } else {
    value = String(fact.value);
  }
  return msg('evaluations.detailField', '{label}: {value}', { label: detailLabel(fact.key, msg), value });
}

function detailLabel(key: string, msg: ReturnType<typeof useI18n>['msg']) {
  switch (key) {
    case 'pass':
      return msg('evaluations.detailPass', 'Pass');
    case 'model':
      return msg('evaluations.detailModel', 'Model');
    case 'duration_seconds':
      return msg('evaluations.detailDuration', 'Duration');
    case 'slowest_seconds':
      return msg('evaluations.detailSlowest', 'Slowest');
    case 'input_tokens':
      return msg('evaluations.detailInputLabel', 'Input');
    case 'output_tokens':
      return msg('evaluations.detailOutputLabel', 'Output');
    default:
      return key;
  }
}

function caseLabel(experiment: EvaluationExperiment, names: ExperimentNames, itemId: string) {
  const item = experiment.items?.find((entry) => entry.id === itemId);
  const caseId = item?.case_id ?? itemId;
  return displayName(names.cases, caseId);
}

function ExperimentCaseResults({
  experiment,
  names,
  cases,
  workspaceId,
  openId,
  onOpenId,
}: {
  experiment: EvaluationExperiment;
  names: ExperimentNames;
  cases: EvaluationCase[];
  workspaceId: string;
  openId: string;
  onOpenId: (itemId: string) => void;
}) {
  const { msg } = useI18n();
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('');
  const rows = visibleCaseRows(caseRows(experiment), query, status, names.cases);
  const evaluators = evaluatorSummaries(experiment);
  const open = rows.find((row) => row.id === openId) ?? caseRows(experiment).find((row) => row.id === openId) ?? null;
  const viewCase = msg('evaluations.viewCase', 'View');
  return (
    <div>
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="relative max-w-xs flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            aria-label={msg('evaluations.searchCases', 'Search cases')}
            className="h-8 pl-8"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <CaseStatusFilter value={status} onChange={setStatus} />
      </div>
      <div className="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('evaluations.caseId', 'Case ID')}</TableHead>
              <TableHead>{msg('evaluations.testCase', 'Case')}</TableHead>
              <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
              {evaluators.map((summary) => (
                <TableHead key={summary.evaluatorId}>{displayName(names.evaluators, summary.evaluatorId)}</TableHead>
              ))}
              <TableHead className="text-right">{msg('evaluations.action', 'Action')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableEmptyRow colSpan={4 + evaluators.length}>
                {msg('evaluations.noCases', 'No cases yet.')}
              </TableEmptyRow>
            ) : (
              rows.map((row) => (
                <CaseResultRow
                  key={row.id}
                  row={row}
                  names={names}
                  evaluators={evaluators.map((summary) => summary.evaluatorId)}
                  viewLabel={viewCase}
                  onView={() => onOpenId(row.id)}
                />
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <CaseResultDrawer
        row={open}
        caseItem={cases.find((item) => item.id === open?.caseId)}
        names={names}
        workspaceId={workspaceId}
        onOpenChange={(next) => {
          if (!next) {
            onOpenId('');
          }
        }}
      />
    </div>
  );
}

function CaseResultRow({
  row,
  names,
  evaluators,
  viewLabel,
  onView,
}: {
  row: ExperimentCaseRow;
  names: ExperimentNames;
  evaluators: string[];
  viewLabel: string;
  onView: () => void;
}) {
  const caseName = displayName(names.cases, row.caseId);
  return (
    <TableRow>
      <TableCell className="max-w-40 truncate font-mono text-xs text-muted-foreground">{row.caseId}</TableCell>
      <TableCell className="max-w-64 truncate">{caseName}</TableCell>
      <TableCell>
        {row.error && row.status === 'failed' ? (
          <span className="text-destructive">{row.error}</span>
        ) : (
          <EvaluationStatusBadge status={row.status} />
        )}
      </TableCell>
      {evaluators.map((evaluatorId) => {
        const score = row.scores.find((item) => item.evaluator_id === evaluatorId);
        return (
          <TableCell key={evaluatorId} className="tabular-nums">
            {scoreText(score)}
          </TableCell>
        );
      })}
      <TableCell className="text-right">
        <button
          type="button"
          className="text-sm text-muted-foreground hover:text-foreground"
          aria-label={`${viewLabel} ${row.caseId}`}
          onClick={onView}
        >
          {viewLabel}
        </button>
      </TableCell>
    </TableRow>
  );
}

function scoreText(score: EvaluationExperimentScore | undefined) {
  if (!score) {
    return '—';
  }
  if (score.status === 'failed') {
    return score.error || '—';
  }
  return formatScore(score.score) || '—';
}

function CaseStatusFilter({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { msg } = useI18n();
  const options: Array<{ value: string; label: string }> = [
    { value: '', label: msg('evaluations.allStatuses', 'All statuses') },
    { value: 'pending', label: msg('evaluations.statusPending', 'Queued') },
    { value: 'running', label: msg('evaluations.statusRunning', 'Running') },
    { value: 'completed', label: msg('evaluations.statusCompleted', 'Completed') },
    { value: 'failed', label: msg('evaluations.statusFailed', 'Failed') },
    { value: 'cancelled', label: msg('evaluations.statusCancelled', 'Cancelled') },
  ];
  return (
    <select
      aria-label={msg('evaluations.status', 'Status')}
      className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm"
      value={value}
      onChange={(event) => onChange(event.target.value)}
    >
      {options.map((option) => (
        <option key={option.label} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  );
}

function ExperimentConfig({ experiment, names }: { experiment: EvaluationExperiment; names: ExperimentNames }) {
  const { msg } = useI18n();
  const evaluators = [...(experiment.evaluators ?? [])].sort((left, right) => left.position - right.position);
  return (
    <section className="rounded-lg border px-5 py-5">
      <dl className="grid gap-y-5 text-sm">
        <ConfigRow label={msg('evaluations.target', 'Target')}>
          <span className="block">{names.agent}</span>
          <span className="mt-1 block font-mono text-xs text-muted-foreground">
            {experiment.agent_id} v{experiment.agent_version}
          </span>
        </ConfigRow>
        <ConfigRow label={msg('evaluations.environmentId', 'Environment')}>{experiment.environment_id}</ConfigRow>
        <ConfigRow label={msg('evaluations.agentTimeout', 'Timeout (seconds)')}>{experiment.timeout_seconds}</ConfigRow>
        <ConfigRow label={msg('evaluations.datasetField', 'Dataset')}>
          <span className="block">{names.dataset}</span>
          <span className="mt-1 block font-mono text-xs text-muted-foreground">{experiment.version_id}</span>
        </ConfigRow>
        <ConfigRow label={msg('evaluations.evaluators', 'Evaluators')}>
          {evaluators.length === 0 ? (
            '—'
          ) : (
            <span className="grid gap-2">
              {evaluators.map((evaluator) => (
                <span key={evaluator.id}>
                  <span className="block">{displayName(names.evaluators, evaluator.id)}</span>
                  <span className="block font-mono text-xs text-muted-foreground">{evaluator.version_id}</span>
                </span>
              ))}
            </span>
          )}
        </ConfigRow>
        {experiment.env ? (
          <ConfigRow label={msg('evaluations.env', 'Environment variables')}>
            <pre className="whitespace-pre-wrap font-mono text-xs text-muted-foreground">
              {JSON.stringify(experiment.env, null, 2)}
            </pre>
          </ConfigRow>
        ) : null}
        {experiment.resources ? (
          <ConfigRow label={msg('evaluations.resources', 'Resources')}>
            <pre className="whitespace-pre-wrap font-mono text-xs text-muted-foreground">
              {JSON.stringify(experiment.resources, null, 2)}
            </pre>
          </ConfigRow>
        ) : null}
      </dl>
    </section>
  );
}

function ConfigRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[9rem_minmax(0,1fr)] gap-x-6">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}
