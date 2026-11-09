import type { EvaluationItem, EvaluationSummary } from './api';

export function failedCount(summary: Pick<EvaluationSummary, 'scored' | 'passed'> | undefined) {
  if (!summary) return 0;
  return Math.max(0, summary.scored - summary.passed);
}

export function averageDimensions(items: EvaluationItem[] | undefined) {
  const totals = new Map<string, { sum: number; count: number }>();
  for (const item of items ?? []) {
    const dimensions = item.score?.dimensions;
    if (!Array.isArray(dimensions)) continue;
    for (const dimension of dimensions) {
      if (!dimension?.name || typeof dimension.score !== 'number') continue;
      const current = totals.get(dimension.name) ?? { sum: 0, count: 0 };
      current.sum += dimension.score;
      current.count += 1;
      totals.set(dimension.name, current);
    }
  }
  return [...totals.entries()].map(([name, value]) => ({ name, score: value.sum / value.count }));
}

export function filterEvaluationTasks<T extends { name: string; agent_id: string; status: string }>(
  tasks: T[],
  query: string,
  status: string,
) {
  const needle = query.trim().toLowerCase();
  return tasks.filter((task) => {
    if (status !== 'all' && task.status !== status) return false;
    if (!needle) return true;
    return task.name.toLowerCase().includes(needle) || task.agent_id.toLowerCase().includes(needle);
  });
}
