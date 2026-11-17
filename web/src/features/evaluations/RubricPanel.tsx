import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Scale } from 'lucide-react';
import { useMemo, useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { ResourceListPagination } from '../../shared/ui/resource-list-pagination';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { deleteEvaluationRubric, evaluationError, listEvaluationRubrics, type EvaluationRubric } from './api';
import { CopyButton } from '../../shared/ui/copy-button';
import { EvaluationEmpty, EvaluationSearch, formatEvaluationTime, shortenId } from './evaluationChrome';
import { RubricDetailDialog } from './RubricDetailDialog';
import { useEvaluationCursor } from './useEvaluationCursor';
import { useEvaluationScope } from './useEvaluationScope';

export function RubricPanel({ onCreate }: { onCreate: () => void }) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<EvaluationRubric | null>(null);
  const cursor = useEvaluationCursor(`${scope.orgUuid}:${scope.workspaceId}:rubrics`);
  const rubrics = useQuery({
    queryKey: ['evaluation-rubrics', scope.orgUuid, scope.workspaceId, cursor.page ?? ''],
    queryFn: () => listEvaluationRubrics(scope.orgUuid, scope.workspaceId, cursor.page),
    enabled: scope.ready,
  });
  const remove = useMutation({
    mutationFn: (rubricId: string) => deleteEvaluationRubric(scope.orgUuid, scope.workspaceId, rubricId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['evaluation-rubrics', scope.orgUuid, scope.workspaceId] });
    },
  });
  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (rubrics.data?.data ?? []).filter((rubric) => {
      if (!needle) return true;
      return (
        rubric.name.toLowerCase().includes(needle) ||
        rubric.id.toLowerCase().includes(needle) ||
        rubric.body.toLowerCase().includes(needle)
      );
    });
  }, [query, rubrics.data]);
  if (rubrics.isLoading)
    return <p className="text-sm text-muted-foreground">{msg('evaluations.loading', 'Loading')}</p>;
  if (rubrics.isError) return <p className="text-sm text-destructive">{evaluationError(rubrics.error)}</p>;
  return (
    <div>
      <EvaluationSearch
        value={query}
        onChange={setQuery}
        placeholder={msg('evaluations.searchRubrics', 'Search rubric name, ID, or content')}
      />
      {rows.length === 0 ? (
        <EvaluationEmpty
          icon={Scale}
          title={
            query
              ? msg('evaluations.noRubricMatch', 'No matching rubrics')
              : msg('evaluations.emptyRubrics', 'No rubrics yet')
          }
          body={
            query
              ? msg('evaluations.noMatchBody', 'Try another keyword or clear the filter.')
              : msg('evaluations.emptyRubricsBody', 'Create a rubric before scoring an evaluation.')
          }
          action={msg('evaluations.createRubric', 'Create rubric')}
          onAction={onCreate}
        />
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border">
          <Table className="min-w-[1080px]">
            <TableHeader>
              <TableRow>
                <TableHead className="h-10 px-3">{msg('evaluations.id', 'ID')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.rubrics', 'Rubrics')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.version', 'Version')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.status', 'Status')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.summary', 'Summary')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.createdAt', 'Created')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.actions', 'Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((rubric) => (
                <TableRow key={rubric.id}>
                  <TableCell className="h-11 px-3">
                    <span className="inline-flex items-center gap-1 font-mono text-xs">
                      {shortenId(rubric.id)}
                      <CopyButton value={rubric.id} label={msg('evaluations.copyId', 'Copy ID')} />
                    </span>
                  </TableCell>
                  <TableCell className="h-11 px-3">{rubric.name}</TableCell>
                  <TableCell className="h-11 px-3 tabular-nums">v{rubric.version}</TableCell>
                  <TableCell className="h-11 px-3">
                    <span className="rounded-full bg-[var(--success-bg)] px-2 py-0.5 text-xs text-[var(--green-11)]">
                      {msg('evaluations.available', 'Available')}
                    </span>
                  </TableCell>
                  <TableCell className="h-11 max-w-[360px] truncate px-3 text-muted-foreground">
                    {rubric.body}
                  </TableCell>
                  <TableCell className="h-11 px-3 tabular-nums">{formatEvaluationTime(rubric.created_at)}</TableCell>
                  <TableCell className="h-11 px-3">
                    <button type="button" className="mr-3 text-sm text-primary" onClick={() => setSelected(rubric)}>
                      {msg('evaluations.view', 'View')}
                    </button>
                    <button
                      type="button"
                      className="text-sm text-primary"
                      onClick={() => {
                        if (
                          window.confirm(
                            msg('evaluations.confirmDeleteRubric', 'Delete this rubric? This cannot be undone.'),
                          )
                        ) {
                          remove.mutate(rubric.id);
                        }
                      }}
                    >
                      {msg('evaluations.delete', 'Delete')}
                    </button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {cursor.pageIndex > 0 || rubrics.data?.next_page ? (
        <ResourceListPagination
          currentPage={cursor.pageIndex + 1}
          totalPages={null}
          canPrevious={cursor.pageIndex > 0 && !rubrics.isFetching}
          canNext={Boolean(rubrics.data?.next_page) && !rubrics.isFetching}
          onPrevious={cursor.previous}
          onNext={() => cursor.next(rubrics.data?.next_page ?? null)}
        />
      ) : null}
      <RubricDetailDialog
        rubric={selected}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
        orgUuid={scope.orgUuid}
        workspaceId={scope.workspaceId}
      />
    </div>
  );
}
