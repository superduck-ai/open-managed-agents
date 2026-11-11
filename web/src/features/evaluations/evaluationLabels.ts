import type { ItemOutcome } from './taskReport';

type Message = (id: string, fallback: string) => string;

const taskStatuses = {
  pending: ['evaluations.statusPending', 'Queued'],
  running: ['evaluations.statusRunning', 'Running'],
  completed: ['evaluations.statusCompleted', 'Completed'],
  failed: ['evaluations.statusFailed', 'Failed'],
  cancelled: ['evaluations.statusCancelled', 'Stopped'],
} as const;

const itemOutcomes: Record<ItemOutcome, readonly [string, string]> = {
  passed: ['evaluations.passed', 'Passed'],
  failed: ['evaluations.failed', 'Failed'],
  error: ['evaluations.systemError', 'System error'],
  running: ['evaluations.inProgress', 'In progress'],
  unscored: ['evaluations.notScored', 'Not scored'],
};

const statusTones: Record<string, string> = {
  pending: 'bg-muted text-muted-foreground',
  running: 'bg-[var(--blue-3)] text-[var(--blue-11)]',
  completed: 'bg-[var(--success-bg)] text-[var(--green-11)]',
  failed: 'bg-destructive/10 text-destructive',
  cancelled: 'bg-muted text-muted-foreground',
};

export const OUTCOME_TONES: Record<ItemOutcome, { text: string; fill: string }> = {
  passed: { text: 'bg-[var(--success-bg)] text-[var(--green-11)]', fill: 'bg-[var(--green-9)]' },
  failed: { text: 'bg-[var(--warning-bg)] text-[var(--amber-11)]', fill: 'bg-[var(--amber-9)]' },
  error: { text: 'bg-destructive/10 text-destructive', fill: 'bg-destructive' },
  running: { text: 'bg-muted text-muted-foreground', fill: 'bg-muted-foreground/40' },
  unscored: { text: 'bg-muted text-muted-foreground', fill: 'bg-muted-foreground/70' },
};

function labeled(table: Record<string, readonly [string, string]>, value: string, msg: Message) {
  const entry = table[value];
  return entry ? msg(entry[0], entry[1]) : value;
}

export function evaluationStatusLabel(status: string, msg: Message) {
  return labeled(taskStatuses, status, msg);
}

export function evaluationStatusTone(status: string) {
  return statusTones[status] ?? statusTones.pending;
}

export function itemOutcomeLabel(outcome: ItemOutcome, msg: Message) {
  return labeled(itemOutcomes, outcome, msg);
}
