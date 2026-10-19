import { Ban, Check, CircleX, Clock3, Loader2, Pause, Square, type LucideIcon } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import type { SessionExecutionState } from '../types';
import { sessionExecutionStateLabel } from './sessionExecutionState';
import clsx from 'clsx';

const stateIcons: Record<SessionExecutionState, LucideIcon> = {
  running: Loader2,
  awaiting_approval: Clock3,
  awaiting_result: Clock3,
  awaiting_action: Clock3,
  completed: Check,
  denied: Ban,
  failed: CircleX,
  interrupted: Square,
  stopped: Square,
  stopping: Loader2,
  queued: Clock3,
  rescheduled: Clock3,
  budget_reached: Pause,
  terminated: Ban,
};

export function SessionExecutionStateBadge({ state, tool = false }: { state: SessionExecutionState; tool?: boolean }) {
  const { msg } = useI18n();
  const Icon = stateIcons[state];
  const pending = state === 'running' || state === 'stopping';
  return (
    <Badge
      variant="secondary"
      data-execution-state={state}
      data-tool-lifecycle={tool ? state : undefined}
      role={pending ? 'status' : undefined}
      className={clsx(
        'h-4 shrink-0 gap-1 rounded px-1 py-0 text-[10px] leading-none font-medium',
        state === 'failed' ? 'bg-destructive/10 text-destructive' : 'bg-secondary text-secondary-foreground',
      )}
    >
      <Icon className={clsx('size-2.5!', pending && 'animate-spin')} aria-hidden />
      {sessionExecutionStateLabel(state, msg)}
    </Badge>
  );
}
