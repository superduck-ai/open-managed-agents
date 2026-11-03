import { Database, Plus, Search } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationDataset } from './api';
import { CopyIdButton } from './CopyIdButton';
import { CreateDatasetDialog } from './CreateDatasetDialog';
import { DatasetPresets } from './DatasetPresets';
import { EvaluationEmpty } from './EvaluationEmpty';
import { EvaluationOriginTabs, type EvaluationOrigin } from './EvaluationOriginTabs';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import { evaluationDatasetHref, useEvaluationDatasetList } from './useEvaluationDatasets';

type DatasetStatusFilter = 'available' | 'archived' | 'all';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationDatasetList();
  const [createOpen, setCreateOpen] = useState(false);
  const [section, setSection] = useState<EvaluationOrigin>('custom');
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<DatasetStatusFilter>('available');
  const datasets = query.data?.data ?? [];
  const visible = datasets.filter(
    (dataset) =>
      (status === 'all' || dataset.status === status) && matchesQuery(search, [dataset.id, dataset.name, dataset.desc]),
  );
  const showEmpty =
    section === 'custom' && workspace.workspaceReady && !query.isLoading && !query.error && datasets.length === 0;
  const description = msg('evaluations.description', 'Manage datasets and test cases.');

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="datasets" />
      <ResourcePageHeader
        title={msg('evaluations.title', 'Datasets')}
        description={description}
        actions={
          section === 'custom' ? (
            <Button type="button" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" aria-hidden />
              {msg('evaluations.create', 'Create dataset')}
            </Button>
          ) : null
        }
      />
      <EvaluationOriginTabs section={section} onSection={setSection} />
      <div className="mb-3 flex items-center gap-3">
        <div className="relative min-w-0 max-w-sm flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={msg('evaluations.search', 'Search datasets...')}
            aria-label={msg('evaluations.search', 'Search datasets...')}
            className="h-9 bg-background pl-8"
          />
        </div>
        {section === 'custom' ? (
          <select
            aria-label={msg('evaluations.status', 'Status')}
            className="h-9 shrink-0 rounded-md border border-input bg-background px-3 text-sm"
            value={status}
            onChange={(event) => {
              if (isDatasetStatusFilter(event.target.value)) {
                setStatus(event.target.value);
              }
            }}
          >
            <option value="available">{msg('evaluations.statusAvailable', 'Available')}</option>
            <option value="archived">{msg('evaluations.statusArchived', 'Archived')}</option>
            <option value="all">{msg('evaluations.statusAll', 'All')}</option>
          </select>
        ) : null}
      </div>
      {section === 'presets' ? (
        <DatasetPresets search={search} />
      ) : showEmpty ? (
        <EvaluationEmpty
          icon={<Database aria-hidden />}
          title={msg('evaluations.empty', 'No datasets yet.')}
          description={msg('evaluations.emptyHint', 'A dataset shows up here after you create one.')}
          action={
            <Button type="button" variant="outline" onClick={() => setCreateOpen(true)}>
              {msg('evaluations.create', 'Create dataset')}
            </Button>
          }
        />
      ) : (
        <DatasetTable
          datasets={visible}
          workspaceId={workspace.workspaceId}
          isLoading={!workspace.workspaceReady || query.isLoading}
          errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
          onRetry={() => void query.refetch()}
        />
      )}
      <CreateDatasetDialog open={createOpen} onOpenChange={setCreateOpen} />
    </section>
  );
}

function DatasetTable({
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
  return (
    <Table className="table-fixed">
      <TableHeader>
        <TableRow>
          <TableHead className="w-[14%] text-xs font-normal text-muted-foreground">
            {msg('evaluations.id', 'ID')}
          </TableHead>
          <TableHead className="w-[16%] text-xs font-normal text-muted-foreground">
            {msg('evaluations.name', 'Name')}
          </TableHead>
          <TableHead className="text-xs font-normal text-muted-foreground">
            {msg('evaluations.descriptionField', 'Description')}
          </TableHead>
          <TableHead className="w-20 text-xs font-normal text-muted-foreground">
            {msg('evaluations.versionColumn', 'Version')}
          </TableHead>
          <TableHead className="w-24 text-xs font-normal text-muted-foreground">
            {msg('evaluations.status', 'Status')}
          </TableHead>
          <TableHead className="w-16 text-xs font-normal text-muted-foreground">
            {msg('evaluations.caseCount', 'Cases')}
          </TableHead>
          <TableHead className="w-28 text-xs font-normal text-muted-foreground">
            {msg('evaluations.createdAt', 'Created')}
          </TableHead>
          <TableHead className="w-24 text-right text-xs font-normal text-muted-foreground">
            {msg('evaluations.actions', 'Action')}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <DatasetRows
          datasets={datasets}
          workspaceId={workspaceId}
          isLoading={isLoading}
          errorMessage={errorMessage}
          onRetry={onRetry}
        />
      </TableBody>
    </Table>
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
    return <TableLoadingRow colSpan={8} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={8}
        title={msg('evaluations.loadFailed', 'Could not load datasets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (datasets.length === 0) {
    return <TableEmptyRow colSpan={8}>{msg('evaluations.empty', 'No datasets yet.')}</TableEmptyRow>;
  }
  return datasets.map((dataset) => (
    <TableRow key={dataset.id}>
      <TableCell>
        <CopyIdButton id={dataset.id} />
      </TableCell>
      <TableCell className="font-medium">
        <a className="block truncate" href={evaluationDatasetHref(workspaceId, dataset.id)}>
          {dataset.name}
        </a>
      </TableCell>
      <TableCell className="text-muted-foreground">
        <span className="block truncate">{dataset.desc}</span>
      </TableCell>
      <TableCell>v{dataset.version.version}</TableCell>
      <TableCell>
        <EvaluationStatusBadge status={dataset.status} />
      </TableCell>
      <TableCell>{dataset.version.case_count}</TableCell>
      <TableCell className="text-muted-foreground">{dateLabel(dataset.created_at)}</TableCell>
      <TableCell className="text-right">
        <a
          className="text-sm text-muted-foreground hover:text-foreground"
          href={evaluationDatasetHref(workspaceId, dataset.id)}
          aria-label={`${msg('evaluations.viewDetail', 'View')} ${dataset.name}`}
        >
          {msg('evaluations.viewDetail', 'View')}
        </a>
      </TableCell>
    </TableRow>
  ));
}

function isDatasetStatusFilter(value: string): value is DatasetStatusFilter {
  return value === 'available' || value === 'archived' || value === 'all';
}

function matchesQuery(query: string, parts: string[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.some((part) => part.toLowerCase().includes(needle));
}

function dateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}
