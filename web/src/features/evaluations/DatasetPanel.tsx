import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Database } from 'lucide-react';
import { useMemo, useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { CopyButton } from '../../shared/ui/copy-button';
import { ResourceListPagination } from '../../shared/ui/resource-list-pagination';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { deleteEvaluationDataset, evaluationError, listEvaluationDatasets } from './api';
import { datasetPath } from './datasetPath';
import { EvaluationEmpty, EvaluationSearch, formatEvaluationTime, shortenId } from './evaluationChrome';
import { useEvaluationCursor } from './useEvaluationCursor';
import { useEvaluationScope } from './useEvaluationScope';

export function DatasetPanel({ onCreate }: { onCreate: () => void }) {
  const { msg } = useI18n();
  const navigate = useNavigate();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState('');
  const cursor = useEvaluationCursor(`${scope.orgUuid}:${scope.workspaceId}:datasets`);
  const datasets = useQuery({
    queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId, cursor.page ?? ''],
    queryFn: () => listEvaluationDatasets(scope.orgUuid, scope.workspaceId, cursor.page),
    enabled: scope.ready,
  });
  const remove = useMutation({
    mutationFn: (datasetId: string) => deleteEvaluationDataset(scope.orgUuid, scope.workspaceId, datasetId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId] });
    },
  });
  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (datasets.data?.data ?? []).filter((dataset) => {
      if (!needle) return true;
      return dataset.name.toLowerCase().includes(needle) || dataset.id.toLowerCase().includes(needle);
    });
  }, [datasets.data, query]);
  if (datasets.isLoading)
    return <p className="text-sm text-muted-foreground">{msg('evaluations.loading', 'Loading')}</p>;
  if (datasets.isError) return <p className="text-sm text-destructive">{evaluationError(datasets.error)}</p>;
  return (
    <div>
      <EvaluationSearch
        value={query}
        onChange={setQuery}
        placeholder={msg('evaluations.searchDatasets', 'Search dataset name or ID')}
      />
      {rows.length === 0 ? (
        <EvaluationEmpty
          icon={Database}
          title={
            query
              ? msg('evaluations.noDatasetMatch', 'No matching datasets')
              : msg('evaluations.emptyDatasets', 'No datasets yet')
          }
          body={
            query
              ? msg('evaluations.noMatchBody', 'Try another keyword or clear the filter.')
              : msg('evaluations.emptyDatasetsBody', 'Create a dataset to start an evaluation.')
          }
          action={msg('evaluations.createDataset', 'Create dataset')}
          onAction={onCreate}
        />
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border">
          <Table className="min-w-[1080px]">
            <TableHeader>
              <TableRow>
                <TableHead className="h-10 px-3">{msg('evaluations.id', 'ID')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.datasets', 'Datasets')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.cases', 'Cases')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.status', 'Status')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.createdAt', 'Created')}</TableHead>
                <TableHead className="h-10 px-3">{msg('evaluations.actions', 'Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((dataset) => (
                <TableRow key={dataset.id}>
                  <TableCell className="h-11 px-3">
                    <span className="inline-flex items-center gap-1 font-mono text-xs">
                      {shortenId(dataset.id)}
                      <CopyButton value={dataset.id} label={msg('evaluations.copyId', 'Copy ID')} />
                    </span>
                  </TableCell>
                  <TableCell className="h-11 px-3">{dataset.name}</TableCell>
                  <TableCell className="h-11 px-3 tabular-nums">{dataset.case_count}</TableCell>
                  <TableCell className="h-11 px-3">
                    <span className="rounded-full bg-[var(--success-bg)] px-2 py-0.5 text-xs text-[var(--green-11)]">
                      {msg('evaluations.available', 'Available')}
                    </span>
                  </TableCell>
                  <TableCell className="h-11 px-3 tabular-nums">{formatEvaluationTime(dataset.created_at)}</TableCell>
                  <TableCell className="h-11 px-3">
                    <button
                      type="button"
                      className="mr-3 text-sm text-primary"
                      onClick={() => void navigate({ href: datasetPath(scope.workspaceId, dataset.id) })}
                    >
                      {msg('evaluations.view', 'View')}
                    </button>
                    <button
                      type="button"
                      className="text-sm text-primary"
                      onClick={() => {
                        if (
                          window.confirm(
                            msg('evaluations.confirmDeleteDataset', 'Delete this dataset? This cannot be undone.'),
                          )
                        ) {
                          remove.mutate(dataset.id);
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
      {cursor.pageIndex > 0 || datasets.data?.next_page ? (
        <ResourceListPagination
          currentPage={cursor.pageIndex + 1}
          totalPages={null}
          canPrevious={cursor.pageIndex > 0 && !datasets.isFetching}
          canNext={Boolean(datasets.data?.next_page) && !datasets.isFetching}
          onPrevious={cursor.previous}
          onNext={() => cursor.next(datasets.data?.next_page ?? null)}
        />
      ) : null}
    </div>
  );
}
