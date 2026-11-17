import { ChevronDown } from 'lucide-react';

import { useI18n } from '../../shared/i18n';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { CopyButton } from '../../shared/ui/copy-button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import type { EvaluationTask } from './api';
import { formatEvaluationTime, shortenId } from './evaluationChrome';
import { evaluationStatusLabel, evaluationStatusTone } from './evaluationLabels';

export type TaskAction = 'cancel' | 'retry';

export function TaskDetailHeader({
  taskId,
  task,
  busy,
  onBack,
  onAction,
}: {
  taskId: string;
  task: EvaluationTask | undefined;
  busy: boolean;
  onBack: () => void;
  onAction: (action: TaskAction) => void;
}) {
  const { msg } = useI18n();
  const title = task?.name || msg('evaluations.taskType', 'Evaluation task');
  return (
    <header className="mb-5">
      <p className="mb-3 text-sm text-muted-foreground">
        <button type="button" className="hover:text-foreground" onClick={onBack}>
          {msg('evaluations.title', 'Evaluations')}
        </button>
        <span> / </span>
        <span className="font-mono text-foreground">{shortenId(taskId)}</span>
      </p>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-2xl leading-8 font-medium">{title}</h1>
            <Badge variant="outline">{msg('evaluations.taskType', 'Evaluation task')}</Badge>
            {task ? (
              <Badge variant="secondary" className={evaluationStatusTone(task.status)}>
                {evaluationStatusLabel(task.status, msg)}
              </Badge>
            ) : null}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1 font-mono">
              {shortenId(taskId)}
              <CopyButton value={taskId} label={msg('evaluations.copyId', 'Copy ID')} />
            </span>
            {task ? (
              <span className="tabular-nums">
                {msg('evaluations.createdAt', 'Created')} {formatEvaluationTime(task.created_at)}
              </span>
            ) : null}
          </div>
        </div>
        {task ? <TaskActionMenu task={task} busy={busy} onAction={onAction} /> : null}
      </div>
    </header>
  );
}

function TaskActionMenu({
  task,
  busy,
  onAction,
}: {
  task: EvaluationTask;
  busy: boolean;
  onAction: (action: TaskAction) => void;
}) {
  const { msg } = useI18n();
  const cancellable = task.status === 'pending' || task.status === 'running';
  const retryable = task.status === 'completed' || task.status === 'failed';
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button type="button" variant="outline" disabled={busy || (!cancellable && !retryable)} />}
      >
        {msg('evaluations.actions', 'Actions')}
        <ChevronDown data-icon="inline-end" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {retryable ? (
          <DropdownMenuItem onClick={() => onAction('retry')}>
            {msg('evaluations.retry', 'Retry failed')}
          </DropdownMenuItem>
        ) : null}
        {cancellable ? (
          <DropdownMenuItem variant="destructive" onClick={() => onAction('cancel')}>
            {msg('evaluations.cancel', 'Cancel')}
          </DropdownMenuItem>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
