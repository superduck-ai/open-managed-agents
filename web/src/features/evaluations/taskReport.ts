import type { EvaluationItem, EvaluationTask } from './api';
import { csvCell } from './datasetSample';

export type ItemOutcome = 'passed' | 'failed' | 'error' | 'running' | 'unscored';
export type ItemFilter = 'all' | ItemOutcome;
export type TaskConclusion = 'running' | 'cancelled' | 'empty' | 'incomplete' | 'complete';

export const ITEM_OUTCOMES: ItemOutcome[] = ['passed', 'failed', 'error', 'running', 'unscored'];

function taskOpen(status: string) {
  return status === 'pending' || status === 'running';
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

export function itemOutcome(item: Pick<EvaluationItem, 'status' | 'score'>, taskStatus: string): ItemOutcome {
  if (item.status === 'cancelled') return 'unscored';
  if (item.status === 'pending' || item.status === 'running' || (item.status === 'completed' && !item.score)) {
    return taskOpen(taskStatus) ? 'running' : 'unscored';
  }
  if (item.status !== 'completed' || item.score?.status !== 'completed') return 'error';
  return item.score.passed ? 'passed' : 'failed';
}

export function outcomeCounts(items: EvaluationItem[] | undefined, taskStatus: string) {
  const counts: Record<ItemOutcome, number> = { passed: 0, failed: 0, error: 0, running: 0, unscored: 0 };
  for (const item of items ?? []) counts[itemOutcome(item, taskStatus)] += 1;
  return counts;
}

export function filterItems(items: EvaluationItem[] | undefined, filter: ItemFilter, taskStatus: string) {
  const rows = items ?? [];
  if (filter === 'all') return rows;
  return rows.filter((item) => itemOutcome(item, taskStatus) === filter);
}

export function taskConclusion(
  task: Pick<EvaluationTask, 'status'>,
  counts: Record<ItemOutcome, number>,
): TaskConclusion {
  if (task.status === 'cancelled') return 'cancelled';
  if (taskOpen(task.status) || counts.running > 0) return 'running';
  if (counts.passed + counts.failed === 0) return 'empty';
  if (counts.error > 0) return 'incomplete';
  return 'complete';
}

export function percentOf(count: number, total: number) {
  if (total <= 0) return 0;
  return Math.round((count / total) * 100);
}

export function itemReason(item: EvaluationItem) {
  return item.score?.explanation || item.score?.error_message || item.error_message;
}

export function itemsCsv(
  task: Pick<EvaluationTask, 'status'> & { items?: EvaluationItem[] },
  headers: string[],
  outcomeLabel: (outcome: ItemOutcome) => string,
) {
  const rows = (task.items ?? []).map((item) => [
    item.query,
    item.score?.answer ?? '',
    item.score?.status === 'completed' ? String(item.score.score) : '',
    outcomeLabel(itemOutcome(item, task.status)),
    itemReason(item),
    item.expected_answer,
    item.acceptance,
    item.session_id,
  ]);
  return `${[headers, ...rows].map((row) => row.map(csvCell).join(',')).join('\n')}\n`;
}
