import { useI18n } from '../../shared/i18n';
import { formatPassRate, formatScore, type EvaluationTask } from './api';
import { averageDimensions, failedCount } from './taskReport';

export function TaskReportView({ data }: { data: EvaluationTask }) {
  const { msg } = useI18n();
  const dimensions = averageDimensions(data.items);
  return (
    <div className="grid gap-4">
      <section className="grid gap-3 sm:grid-cols-4">
        <Metric label={msg('evaluations.score', 'Score')} value={formatScore(data.summary?.score)} />
        <Metric label={msg('evaluations.passRate', 'Pass rate')} value={formatPassRate(data.summary?.pass_rate)} />
        <Metric
          label={msg('evaluations.passed', 'Passed')}
          value={`${data.summary?.passed ?? 0}/${data.summary?.scored ?? 0}`}
        />
        <Metric
          label={msg('evaluations.exceptions', 'Exceptions')}
          value={`${data.summary?.exceptions ?? 0}/${data.summary?.total ?? 0}`}
        />
      </section>
      <section className="rounded-xl border border-border p-4">
        <h2 className="text-sm font-medium">{msg('evaluations.distribution', 'Result distribution')}</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {msg('evaluations.distributionHint', 'Exceptions stay out of the pass rate.')}
        </p>
        <div className="mt-3 grid gap-2 text-sm sm:grid-cols-3">
          <p>
            {msg('evaluations.passed', 'Passed')} {data.summary?.passed ?? 0}
          </p>
          <p>
            {msg('evaluations.failed', 'Failed')} {failedCount(data.summary)}
          </p>
          <p>
            {msg('evaluations.exceptions', 'Exceptions')} {data.summary?.exceptions ?? 0}
          </p>
        </div>
      </section>
      {dimensions.length > 0 ? (
        <section className="rounded-xl border border-border p-4">
          <h2 className="text-sm font-medium">{msg('evaluations.dimensions', 'Dimension scores')}</h2>
          <ul className="mt-3 grid gap-3">
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
        </section>
      ) : null}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg tabular-nums">{value}</div>
    </div>
  );
}
