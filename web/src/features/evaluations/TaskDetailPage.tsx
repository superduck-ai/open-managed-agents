import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';

import { useI18n } from '../../shared/i18n';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { evaluationError, getEvaluationTask, postEvaluationTaskAction } from './api';
import { TaskDetailHeader, type TaskAction } from './TaskDetailHeader';
import { TaskInfoPanel } from './TaskInfoPanel';
import { TaskReportView } from './TaskReportView';
import { useEvaluationScope } from './useEvaluationScope';

export function TaskDetailPage() {
  const { msg } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const taskId = decodeURIComponent(location.pathname.match(/\/evaluations\/tasks\/([^/]+)/)?.[1] ?? '');
  const queryKey = ['evaluation-task', scope.orgUuid, scope.workspaceId, taskId];
  const task = useQuery({
    queryKey,
    queryFn: () => getEvaluationTask(scope.orgUuid, scope.workspaceId, taskId),
    enabled: scope.ready && Boolean(taskId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
  const action = useMutation({
    mutationFn: (name: TaskAction) => postEvaluationTaskAction(scope.orgUuid, scope.workspaceId, taskId, name),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey });
    },
  });
  const workspacePath = `/workspaces/${encodeURIComponent(scope.workspaceId)}`;
  const data = task.data;
  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <TaskDetailHeader
        taskId={taskId}
        task={data}
        busy={action.isPending}
        onBack={() => void navigate({ href: `${workspacePath}/evaluations` })}
        onAction={(name) => action.mutate(name)}
      />
      {task.isError ? <p className="mb-4 text-sm text-destructive">{evaluationError(task.error)}</p> : null}
      {action.isError ? <p className="mb-4 text-sm text-destructive">{evaluationError(action.error)}</p> : null}
      {data ? (
        <Tabs defaultValue="info">
          <TabsList variant="line" className="mb-5 w-full justify-start gap-4 border-b border-border">
            <TabsTrigger value="info" className="flex-none px-0">
              {msg('evaluations.taskInfo', 'Task info')}
            </TabsTrigger>
            <TabsTrigger value="report" className="flex-none px-0">
              {msg('evaluations.taskReport', 'Report')}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="info">
            <TaskInfoPanel task={data} />
          </TabsContent>
          <TabsContent value="report">
            <TaskReportView
              task={data}
              onOpenSession={(sessionId) =>
                void navigate({ href: `${workspacePath}/sessions/${encodeURIComponent(sessionId)}` })
              }
            />
          </TabsContent>
        </Tabs>
      ) : null}
    </section>
  );
}
