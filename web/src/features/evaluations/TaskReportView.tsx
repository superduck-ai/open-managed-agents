import { TriangleAlert } from 'lucide-react';

import { useI18n } from '../../shared/i18n';
import { formatPassRate, formatScore, type EvaluationTask } from './api';
import { itemOutcomeLabel, OUTCOME_TONES } from './evaluationLabels';
import { TaskItemsTable } from './TaskItemsTable';
import { TaskSection } from './TaskSection';
import {
  averageDimensions,
  ITEM_OUTCOMES,
  outcomeCounts,
  percentOf,
  taskConclusion,
  type ItemOutcome,
  type TaskConclusion,
} from './taskReport';

const conclusions: Record<TaskConclusion, readonly [string, string]> = {
  running: ['evaluations.conclusionRunning', 'Evaluation in progress'],
  cancelled: ['evaluations.conclusionCancelled', 'Task stopped'],
  empty: ['evaluations.conclusionEmpty', 'No scored results'],
  incomplete: ['evaluations.conclusionIncomplete', 'Finished, but results are incomplete'],
  complete: ['evaluations.conclusionComplete', 'Evaluation complete'],
};

function ScoreRing({ score, label }: { score: number | null | undefined; label: string }) {
  const radius = 34;
  const circumference = 2 * Math.PI * radius;
  const ratio = score == null ? 0 : Math.max(0, Math.min(100, score)) / 100;
  return (
    <div className="grid justify-items-center gap-1">
      <div className="relative size-24">
        <svg viewBox="0 0 80 80" className="size-24 -rotate-90" aria-hidden>
          <circle cx="40" cy="40" r={radius} fill="none" strokeWidth="6" className="stroke-muted" />
          <circle
            cx="40"
            cy="40"
            r={radius}
            fill="none"
            strokeWidth="6"
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={circumference * (1 - ratio)}
            className="stroke-foreground"
          />
        </svg>
        <span className="absolute inset-0 grid place-items-center text-xl font-medium tabular-nums">
          {formatScore(score)}
        </span>
      </div>
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
  );
}

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="grid content-start gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-lg leading-6 font-medium tabular-nums">{value}</span>
      {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
    </div>
  );
}

function OverallCard({ task, counts }: { task: EvaluationTask; counts: Record<ItemOutcome, number> }) {
  const { msg } = useI18n();
  const conclusion = conclusions[taskConclusion(task, counts)];
  const total = task.summary?.total ?? 0;
  return (
    <div className="grid gap-6 rounded-xl border border-border p-5 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1.2fr)] lg:items-center">
      <div className="grid gap-2">
        <span className="text-xs text-muted-foreground">{msg('evaluations.conclusion', 'Conclusion')}</span>
        <p className="text-lg leading-7 font-medium">{msg(conclusion[0], conclusion[1])}</p>
        <p className="text-sm text-muted-foreground">
          {msg(
            'evaluations.conclusionDetail',
            '{total} cases: {passed} passed, {failed} failed, {error} system errors, {running} in progress, {unscored} not scored.',
            { total, ...counts },
          )}
        </p>
      </div>
      <ScoreRing score={task.summary?.score} label={msg('evaluations.averageScore', 'Average score')} />
      <div className="grid grid-cols-2 gap-x-6 gap-y-4">
        <Metric label={msg('evaluations.totalCases', 'Cases')} value={String(total)} />
        <Metric label={msg('evaluations.passedCases', 'Passed cases')} value={String(counts.passed)} />
        <Metric
          label={msg('evaluations.passRate', 'Pass rate')}
          value={formatPassRate(task.summary?.pass_rate)}
          hint={msg('evaluations.passRateHint', 'Passed ÷ scored cases, excluding system errors')}
        />
        <Metric
          label={msg('evaluations.passScore', 'Pass score')}
          value={String(task.pass_score)}
          hint={msg('evaluations.scoredCount', '{count} cases scored', { count: task.summary?.scored ?? 0 })}
        />
      </div>
    </div>
  );
}

function DistributionCard({ counts, total }: { counts: Record<ItemOutcome, number>; total: number }) {
  const { msg } = useI18n();
  return (
    <div className="rounded-xl border border-border p-5">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium">{msg('evaluations.distribution', 'Result distribution')}</h3>
          <p className="text-xs text-muted-foreground">
            {msg('evaluations.distributionHint', 'System errors stay out of the pass rate.')}
          </p>
        </div>
        <span className="text-xs text-muted-foreground tabular-nums">
          {msg('evaluations.caseCount', '{count} cases', { count: total })}
        </span>
      </div>
      <div className="flex h-2 overflow-hidden rounded-full bg-muted">
        {ITEM_OUTCOMES.map((outcome) =>
          counts[outcome] > 0 ? (
            <div
              key={outcome}
              className={OUTCOME_TONES[outcome].fill}
              style={{ width: `${percentOf(counts[outcome], total)}%` }}
            />
          ) : null,
        )}
      </div>
      <ul className="mt-4 grid gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {ITEM_OUTCOMES.map((outcome) => (
          <li key={outcome} className="grid gap-0.5 rounded-lg bg-muted/40 px-3 py-2">
            <span className="flex items-center justify-between gap-2 text-sm">
              <span className="inline-flex items-center gap-1.5">
                <span className={`size-2 rounded-full ${OUTCOME_TONES[outcome].fill}`} aria-hidden />
                {itemOutcomeLabel(outcome, msg)}
              </span>
              <span className="tabular-nums">{counts[outcome]}</span>
            </span>
            <span className="text-xs text-muted-foreground tabular-nums">{percentOf(counts[outcome], total)}%</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function DimensionCard({ dimensions }: { dimensions: Array<{ name: string; score: number }> }) {
  const { msg } = useI18n();
  return (
    <div className="rounded-xl border border-border p-5">
      <h3 className="text-sm font-medium">{msg('evaluations.dimensions', 'Dimension scores')}</h3>
      <ul className="mt-3 grid gap-3 sm:grid-cols-2">
        {dimensions.map((dimension) => (
          <li key={dimension.name}>
            <div className="mb-1 flex justify-between text-xs">
              <span>{dimension.name}</span>
              <span className="tabular-nums">{formatScore(dimension.score)}</span>
            </div>
            <div className="h-1.5 rounded-full bg-muted">
              <div
                className="h-1.5 rounded-full bg-foreground"
                style={{ width: `${Math.max(0, Math.min(100, dimension.score))}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function TaskReportView({ task, onOpenSession }: { task: EvaluationTask; onOpenSession: (id: string) => void }) {
  const { msg } = useI18n();
  const counts = outcomeCounts(task.items, task.status);
  const dimensions = averageDimensions(task.items);
  const total = task.summary?.total ?? 0;
  return (
    <div className="grid gap-8">
      <TaskSection
        index="01"
        title={msg('evaluations.overall', 'Overall result')}
        hint={msg('evaluations.overallHint', 'Score and pass status for this evaluation.')}
      >
        <OverallCard task={task} counts={counts} />
        {counts.error > 0 ? (
          <p className="flex items-start gap-2 rounded-lg bg-[var(--warning-bg)] px-4 py-3 text-sm text-[var(--warning)]">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
            {msg(
              'evaluations.errorBanner',
              '{count} cases hit a system error and were not scored. They stay out of the pass rate.',
              { count: counts.error },
            )}
          </p>
        ) : null}
      </TaskSection>
      <TaskSection
        index="02"
        title={msg('evaluations.quality', 'Quality analysis')}
        hint={msg('evaluations.qualityHint', 'Result distribution and dimension scores.')}
      >
        <DistributionCard counts={counts} total={total} />
        {dimensions.length > 0 ? <DimensionCard dimensions={dimensions} /> : null}
      </TaskSection>
      <TaskItemsTable task={task} counts={counts} onOpenSession={onOpenSession} />
    </div>
  );
}
