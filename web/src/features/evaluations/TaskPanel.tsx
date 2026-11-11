import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { ListChecks } from 'lucide-react';
import { useMemo, useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationError, listEvaluationTasks } from './api';
import { evaluationStatusLabel } from './evaluationLabels';
import { EvaluationEmpty, EvaluationSearch, formatEvaluationTime, shortenId } from './evaluationChrome';
import { EvaluationPagination } from './EvaluationPagination';
import { filterEvaluationTasks } from './taskReport';
import { taskPath } from './taskPath';
import { useEvaluationCursor } from './useEvaluationCursor';
import { useEvaluationScope } from './useEvaluationScope';

export function TaskPanel({ onCreate }: { onCreate: () => void }) {
  const { msg } = useI18n();
  const navigate = useNavigate();
  const scope = useEvaluationScope();
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const cursor = useEvaluationCursor(`${scope.orgUuid}:${scope.workspaceId}:tasks`);
  const tasks = useQuery({
    queryKey: ['evaluation-tasks', scope.orgUuid, scope.workspaceId, cursor.page ?? ''],
    queryFn: () => listEvaluationTasks(scope.orgUuid, scope.workspaceId, cursor.page),
    enabled: scope.ready,
    refetchInterval: (current) =>
      current.state.data?.data.some((task) => task.status === 'running' || task.status === 'pending') ? 3000 : false,
  });
  const rows = useMemo(() => filterEvaluationTasks(tasks.data?.data ?? [], query, status), [query, status, tasks.data]);
  if (tasks.isLoading) return <p className="text-sm text-muted-foreground">{msg('evaluations.loading', 'Loading')}</p>;
  if (tasks.isError) return <p className="text-sm text-destructive">{evaluationError(tasks.error)}</p>;
  return (
    <div>
      <EvaluationSearch
        value={query}
        onChange={setQuery}
        placeholder={msg('evaluations.searchTasks', 'Search tasks or agents')}
        filters={
          <>
            <FilterSelect
              label={msg('evaluations.allStatuses', 'All statuses')}
              value={status}
              onChange={setStatus}
              options={[
                ['all', msg('evaluations.allStatuses', 'All statuses')],
                ['running', msg('evaluations.statusRunning', 'Running')],
                ['completed', msg('evaluations.statusCompleted', 'Completed')],
                ['failed', msg('evaluations.statusFailed', 'Failed')],
                ['cancelled', msg('evaluations.statusCancelled', 'Stopped')],
              ]}
            />
          </>
        }
      />
      {rows.length === 0 ? (
        <EvaluationEmpty
          icon={ListChecks}
          title={
            query
              ? msg('evaluations.noTaskMatch', 'No matching tasks')
              : msg('evaluations.emptyTasks', 'No evaluations yet')
          }
          body={
            query
              ? msg('evaluations.noMatchBody', 'Try another keyword or clear the filter.')
              : msg('evaluations.emptyTasksBody', 'Start an evaluation to score an agent version.')
          }
          action={msg('evaluations.createTask', 'Start evaluation')}
          onAction={onCreate}
        />
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border">
          <Table className="min-w-[920px]">
            <TableHeader>
              <TableRow>
                <TableHead className="h-10 px-3">{msg('evaluations.id', 'ID')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.tasks', 'Tasks')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.agent', 'Agent')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.status', 'Status')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.createdAt', 'Created')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.actions', 'Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((task) => (
                <TableRow key={task.id}>
                  <TableCell className="h-11 px-3 font-mono text-xs">{shortenId(task.id)}</TableCell>
                  <TableCell className="h-11 px-3">
                    <button
                      type="button"
                      className="text-left hover:underline"
                      onClick={() => void navigate({ href: taskPath(scope.workspaceId, task.id) })}
                    >
                      {task.name}
                    </button>
                  </TableCell>
                  <TableCell className="h-11 px-3">{task.agent_id}</TableCell>
                  <TableCell className="h-11 px-3">{evaluationStatusLabel(task.status, msg)}</TableCell>
                  <TableCell className="h-11 px-3 tabular-nums">{formatEvaluationTime(task.created_at)}</TableCell>
                  <TableCell className="h-11 px-3">
                    <button
                      type="button"
                      className="text-sm text-primary"
                      onClick={() => void navigate({ href: taskPath(scope.workspaceId, task.id) })}
                    >
                      {msg('evaluations.view', 'View')}
                    </button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <EvaluationPagination cursor={cursor} nextPage={tasks.data?.next_page} fetching={tasks.isFetching} />
    </div>
  );
}

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<[string, string]>;
  onChange: (value: string) => void;
}) {
  return (
    <Select
      value={value}
      items={options.map(([id, text]) => ({ value: id, label: text }))}
      onValueChange={(next) => onChange(next || 'all')}
    >
      <SelectTrigger aria-label={label} className="h-9 w-[140px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map(([id, text]) => (
          <SelectItem key={id} value={id}>
            {text}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
