import { Plus } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationDataset } from './api';
import { CreateDatasetDialog } from './CreateDatasetDialog';
import { DatasetPresets } from './DatasetPresets';
import { EvaluationOriginTabs, type EvaluationOrigin } from './EvaluationOriginTabs';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { evaluationDatasetHref, useEvaluationDatasetList } from './useEvaluationDatasets';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationDatasetList();
  const [createOpen, setCreateOpen] = useState(false);
  const [section, setSection] = useState<EvaluationOrigin>('custom');
  const datasets = query.data?.data ?? [];

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="datasets" />
      <header className="mb-5 flex items-start justify-between gap-6">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            {msg('evaluations.title', 'Datasets')}
          </h1>
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
            {msg('evaluations.description', 'Reusable cases for the {workspaceName} workspace.', {
              workspaceName: workspace.workspaceName,
            })}
          </p>
        </div>
        {section === 'custom' ? (
          <Button type="button" className="h-9 shrink-0" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden />
            {msg('evaluations.create', 'Create dataset')}
          </Button>
        ) : null}
      </header>
      <EvaluationOriginTabs section={section} onSection={setSection} />
      {section === 'presets' ? (
        <DatasetPresets onReused={() => setSection('custom')} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
              <TableHead>{msg('evaluations.versionColumn', 'Version')}</TableHead>
              <TableHead>{msg('evaluations.caseCount', 'Cases')}</TableHead>
              <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <DatasetRows
              datasets={datasets}
              workspaceId={workspace.workspaceId}
              isLoading={!workspace.workspaceReady || query.isLoading}
              errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
              onRetry={() => void query.refetch()}
            />
          </TableBody>
        </Table>
      )}
      <CreateDatasetDialog open={createOpen} onOpenChange={setCreateOpen} />
    </section>
  );
}

function DatasetRows({
  datasets,
  workspaceId,
  isLoading,
  errorMessage,
  onRetry,
}: {
  datasets: EvaluationDataset[];
  workspaceId: string;
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={4} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={4}
        title={msg('evaluations.loadFailed', 'Could not load datasets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (datasets.length === 0) {
    return <TableEmptyRow colSpan={4}>{msg('evaluations.empty', 'No datasets yet.')}</TableEmptyRow>;
  }
  return datasets.map((dataset) => (
    <TableRow key={dataset.id}>
      <TableCell>
        <a
          className="font-medium text-foreground underline-offset-4 hover:underline"
          href={evaluationDatasetHref(workspaceId, dataset.id)}
        >
          {dataset.name}
        </a>
      </TableCell>
      <TableCell>{dataset.version.version}</TableCell>
      <TableCell>{dataset.version.case_count}</TableCell>
      <TableCell>{datasetStatusLabel(dataset.status, msg)}</TableCell>
    </TableRow>
  ));
}

function datasetStatusLabel(status: EvaluationDataset['status'], msg: ReturnType<typeof useI18n>['msg']) {
  if (status === 'archived') {
    return msg('evaluations.statusArchived', 'Archived');
  }
  return msg('evaluations.statusAvailable', 'Available');
}
