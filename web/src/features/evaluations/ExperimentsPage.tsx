import { FlaskConical, Plus, Search } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { ButtonLink } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationExperiment, type EvaluationExperimentStatus } from './api';
import { CopyIdButton } from './CopyIdButton';
import { EvaluationEmpty } from './EvaluationEmpty';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import {
  evaluationExperimentCompareHref,
  evaluationExperimentHref,
  evaluationExperimentNewHref,
  evaluationExperimentsHref,
  useEvaluationWorkspace,
} from './useEvaluationDatasets';
import { useEvaluationExperimentList } from './useEvaluationExperiments';

export function ExperimentsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationExperimentList();
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<'' | EvaluationExperimentStatus>('');
  const source = query.data?.data ?? [];
  const experiments = source.filter(
    (experiment) =>
      (status === '' || experiment.status === status) &&
      matchesQuery(search, [experiment.id, experiment.name, experiment.agent_id]),
  );
  const showEmpty = workspace.workspaceReady && !query.isLoading && !query.error && source.length === 0;

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <ResourcePageHeader
        title={msg('evaluations.experiments', 'Experiments')}
        description={msg(
          'evaluations.experimentsDescription',
          'Run experiments and score how an agent does on a dataset.',
        )}
        actions={
          <ButtonLink href={evaluationExperimentNewHref(workspace.workspaceId)}>
            <Plus className="size-4" aria-hidden />
            {msg('evaluations.createExperiment', 'New experiment')}
          </ButtonLink>
        }
      />
      <ExperimentModeNav current="list" />
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={msg('evaluations.searchExperiments', 'Search experiments...')}
            aria-label={msg('evaluations.searchExperiments', 'Search experiments...')}
            className="h-8 bg-background pl-8"
          />
        </div>
        <ExperimentStatusFilter value={status} onChange={setStatus} />
      </div>
      {showEmpty ? (
        <EvaluationEmpty
          className="rounded-lg border"
          icon={<FlaskConical aria-hidden />}
          title={msg('evaluations.experimentsEmpty', 'No experiments yet.')}
          description={msg('evaluations.experimentsEmptyHint', 'An experiment shows up here after you create one.')}
          action={
            <ButtonLink href={evaluationExperimentNewHref(workspace.workspaceId)} variant="outline">
              <Plus className="size-4" aria-hidden />
              {msg('evaluations.createFirstExperiment', 'Create the first experiment')}
            </ButtonLink>
          }
        />
      ) : (
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
                {msg('evaluations.agentId', 'Agent')}
              </TableHead>
              <TableHead className="w-24 text-xs font-normal text-muted-foreground">
                {msg('evaluations.evaluators', 'Evaluators')}
              </TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">
                {msg('evaluations.datasetVersion', 'Dataset version')}
              </TableHead>
              <TableHead className="w-28 text-xs font-normal text-muted-foreground">
                {msg('evaluations.status', 'Status')}
              </TableHead>
              <TableHead className="w-28 text-xs font-normal text-muted-foreground">
                {msg('evaluations.createdAt', 'Created')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <ExperimentRows
              experiments={experiments}
              workspaceId={workspace.workspaceId}
              isLoading={!workspace.workspaceReady || query.isLoading}
              errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
              onRetry={() => void query.refetch()}
            />
          </TableBody>
        </Table>
      )}
    </section>
  );
}

function ExperimentRows({
  experiments,
  workspaceId,
  isLoading,
  errorMessage,
  onRetry,
}: {
  experiments: EvaluationExperiment[];
  workspaceId: string;
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={7} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={7}
        title={msg('evaluations.experimentsLoadFailed', 'Could not load experiments')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (experiments.length === 0) {
    return <TableEmptyRow colSpan={7}>{msg('evaluations.experimentsEmpty', 'No experiments yet.')}</TableEmptyRow>;
  }
  return experiments.map((experiment) => (
    <TableRow key={experiment.id}>
      <TableCell>
        <CopyIdButton id={experiment.id} />
      </TableCell>
      <TableCell>
        <a className="block truncate font-medium" href={evaluationExperimentHref(workspaceId, experiment.id)}>
          {experiment.name}
        </a>
      </TableCell>
      <TableCell className="truncate">
        {experiment.agent_id} v{experiment.agent_version}
      </TableCell>
      <TableCell>{experiment.evaluator_count}</TableCell>
      <TableCell>
        <span className="block truncate">{experiment.version_id}</span>
        <span className="block text-xs text-muted-foreground">{experiment.item_count}</span>
      </TableCell>
      <TableCell>
        <EvaluationStatusBadge status={experiment.status} />
      </TableCell>
      <TableCell className="text-muted-foreground">{dateLabel(experiment.created_at)}</TableCell>
    </TableRow>
  ));
}

export function ExperimentModeNav({ current }: { current: 'list' | 'compare' }) {
  const { msg } = useI18n();
  const workspace = useEvaluationWorkspace();
  const items = [
    {
      id: 'list' as const,
      href: evaluationExperimentsHref(workspace.workspaceId),
      label: msg('evaluations.experimentList', 'Experiment list'),
    },
    {
      id: 'compare' as const,
      href: evaluationExperimentCompareHref(workspace.workspaceId),
      label: msg('evaluations.experimentCompare', 'Compare experiments'),
    },
  ];
  return (
    <div className="mb-4 inline-flex gap-1">
      {items.map((item) => {
        const selected = item.id === current;
        return (
          <a
            key={item.id}
            href={item.href}
            aria-current={selected ? 'page' : undefined}
            className={
              selected
                ? 'rounded-md bg-muted px-3 py-1.5 text-sm font-medium text-foreground'
                : 'rounded-md px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground'
            }
          >
            {item.label}
          </a>
        );
      })}
    </div>
  );
}

function ExperimentStatusFilter({
  value,
  onChange,
}: {
  value: '' | EvaluationExperimentStatus;
  onChange: (value: '' | EvaluationExperimentStatus) => void;
}) {
  const { msg } = useI18n();
  const options: Array<{ value: '' | EvaluationExperimentStatus; label: string }> = [
    { value: '', label: msg('evaluations.allStatuses', 'All statuses') },
    { value: 'pending', label: msg('evaluations.statusPending', 'Queued') },
    { value: 'running', label: msg('evaluations.statusRunning', 'Running') },
    { value: 'completed', label: msg('evaluations.statusCompleted', 'Completed') },
    { value: 'failed', label: msg('evaluations.statusFailed', 'Failed') },
    { value: 'cancelled', label: msg('evaluations.statusCancelled', 'Cancelled') },
  ];
  return (
    <select
      aria-label={msg('evaluations.status', 'Status')}
      className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm"
      value={value}
      onChange={(event) => onChange(event.target.value as '' | EvaluationExperimentStatus)}
    >
      {options.map((option) => (
        <option key={option.label} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  );
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
