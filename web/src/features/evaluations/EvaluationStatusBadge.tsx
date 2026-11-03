import { useI18n } from '../../shared/i18n';

export function EvaluationStatusBadge({ status }: { status: string }) {
  const { msg } = useI18n();
  return (
    <span
      className={`inline-flex h-5 items-center rounded-md border px-1.5 text-xs font-medium ${statusClassName(status)}`}
    >
      {statusLabel(status, msg)}
    </span>
  );
}

function statusClassName(status: string) {
  if (status === 'available' || status === 'completed') {
    return 'border-[var(--success)]/30 bg-[var(--success-bg)] text-[var(--success)]';
  }
  if (status === 'pending' || status === 'running') {
    return 'border-[var(--warning)]/30 bg-[var(--warning-bg)] text-[var(--warning)]';
  }
  if (status === 'failed') {
    return 'border-destructive/30 bg-destructive/10 text-destructive';
  }
  return 'border-border bg-muted text-muted-foreground';
}

function statusLabel(status: string, msg: ReturnType<typeof useI18n>['msg']) {
  switch (status) {
    case 'available':
      return msg('evaluations.statusAvailable', 'Available');
    case 'archived':
      return msg('evaluations.statusArchived', 'Archived');
    case 'pending':
      return msg('evaluations.statusPending', 'Queued');
    case 'running':
      return msg('evaluations.statusRunning', 'Running');
    case 'completed':
      return msg('evaluations.statusCompleted', 'Completed');
    case 'failed':
      return msg('evaluations.statusFailed', 'Failed');
    case 'cancelled':
      return msg('evaluations.statusCancelled', 'Cancelled');
    default:
      return status;
  }
}
