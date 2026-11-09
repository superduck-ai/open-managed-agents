import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import {
  evaluationError,
  formatScore,
  getEvaluationTask,
  postEvaluationTaskAction,
  type EvaluationItem,
  type EvaluationTask,
} from './api';
import { useEvaluationScope } from './useEvaluationScope';
import { TaskReportView } from './TaskReportView';
import { evaluationStatusLabel } from './evaluationLabels';

export function TaskDetailPage() {
  const { msg } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const taskId = location.pathname.match(/\/evaluations\/tasks\/([^/]+)/)?.[1] ?? '';
  const task = useQuery({
    queryKey: ['evaluation-task', scope.orgUuid, scope.workspaceId, taskId],
    queryFn: () => getEvaluationTask(scope.orgUuid, scope.workspaceId, decodeURIComponent(taskId)),
    enabled: scope.ready && Boolean(taskId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
  const action = useMutation({
    mutationFn: (name: 'cancel' | 'retry') =>
      postEvaluationTaskAction(scope.orgUuid, scope.workspaceId, decodeURIComponent(taskId), name),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['evaluation-task', scope.orgUuid, scope.workspaceId, taskId] });
    },
  });
  const data = task.data;
  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <ResourcePageHeader
        title={data?.name || msg('evaluations.tasks', 'Tasks')}
        description={
          data ? `${data.agent_id} v${data.agent_version} · ${data.dataset_id} · ${data.rubric_id}` : undefined
        }
        actions={
          <Button
            type="button"
            variant="outline"
            onClick={() => void navigate({ href: `/workspaces/${encodeURIComponent(scope.workspaceId)}/evaluations` })}
          >
            {msg('evaluations.back', 'Back')}
          </Button>
        }
      />
      {task.isError ? <p className="mb-4 text-sm text-destructive">{evaluationError(task.error)}</p> : null}
      {data ? (
        <div className="grid gap-4">
          <p className="text-sm text-muted-foreground">
            {evaluationStatusLabel(data.status, msg)} · {data.agent_id} v{data.agent_version}
          </p>
          <TaskActions
            data={data}
            onAction={(name) => action.mutate(name)}
            labels={{
              cancel: msg('evaluations.cancel', 'Cancel'),
              retry: msg('evaluations.retry', 'Retry failed'),
            }}
          />
          {action.isError ? <p className="text-sm text-destructive">{evaluationError(action.error)}</p> : null}
          <TaskReportView data={data} />
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{msg('evaluations.query', 'Query')}</TableHead>
                <TableHead>{msg('evaluations.score', 'Score')}</TableHead>
                <TableHead>{msg('evaluations.passRate', 'Pass rate')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(data.items ?? []).map((item) => (
                <TaskItemRow
                  key={item.id}
                  item={item}
                  passedLabel={msg('evaluations.passed', 'Passed')}
                  failedLabel={msg('evaluations.failed', 'Failed')}
                  onOpenSession={(sessionId) =>
                    void navigate({
                      href: `/workspaces/${encodeURIComponent(scope.workspaceId)}/sessions/${encodeURIComponent(sessionId)}`,
                    })
                  }
                />
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
    </section>
  );
}

function TaskActions({
  data,
  labels,
  onAction,
}: {
  data: EvaluationTask;
  labels: { cancel: string; retry: string };
  onAction: (name: 'cancel' | 'retry') => void;
}) {
  const running = data.status === 'pending' || data.status === 'running';
  const retryable = data.status === 'completed' || data.status === 'failed';
  return (
    <div className="flex flex-wrap gap-2">
      {running ? (
        <Button type="button" variant="outline" onClick={() => onAction('cancel')}>
          {labels.cancel}
        </Button>
      ) : null}
      {retryable ? (
        <Button type="button" variant="outline" onClick={() => onAction('retry')}>
          {labels.retry}
        </Button>
      ) : null}
    </div>
  );
}

function TaskItemRow({
  item,
  passedLabel,
  failedLabel,
  onOpenSession,
}: {
  item: EvaluationItem;
  passedLabel: string;
  failedLabel: string;
  onOpenSession: (sessionId: string) => void;
}) {
  const scored = item.score?.status === 'completed';
  return (
    <TableRow>
      <TableCell>
        <div>{item.query}</div>
        <div className="text-xs text-muted-foreground">
          {item.score?.explanation || item.error_message || item.status}
        </div>
        {item.session_id ? (
          <button type="button" className="text-xs underline" onClick={() => onOpenSession(item.session_id)}>
            {item.session_id}
          </button>
        ) : null}
      </TableCell>
      <TableCell>{scored ? formatScore(item.score?.score) : '—'}</TableCell>
      <TableCell>{scored ? (item.score?.passed ? passedLabel : failedLabel) : item.status}</TableCell>
    </TableRow>
  );
}
