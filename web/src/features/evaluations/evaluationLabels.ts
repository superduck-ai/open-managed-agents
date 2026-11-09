type Message = (id: string, fallback: string) => string;

const taskStatuses = {
  pending: ['evaluations.statusPending', 'Queued'],
  running: ['evaluations.statusRunning', 'Running'],
  completed: ['evaluations.statusCompleted', 'Completed'],
  failed: ['evaluations.statusFailed', 'Failed'],
  cancelled: ['evaluations.statusCancelled', 'Stopped'],
} as const;

function labeled(table: Record<string, readonly [string, string]>, value: string, msg: Message) {
  const entry = table[value];
  return entry ? msg(entry[0], entry[1]) : value;
}

export function evaluationStatusLabel(status: string, msg: Message) {
  return labeled(taskStatuses, status, msg);
}
