import { Plus } from 'lucide-react';

import { useI18n } from '../../shared/i18n';
import { ButtonLink } from '../../shared/ui/button';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationExperiment } from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import {
  evaluationExperimentCompareHref,
  evaluationExperimentHref,
  evaluationExperimentNewHref,
} from './useEvaluationDatasets';
import { useEvaluationExperimentList } from './useEvaluationExperiments';

export function ExperimentsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationExperimentList();
  const experiments = query.data?.data ?? [];

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <header className="mb-5 flex items-start justify-between gap-6">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            {msg('evaluations.experiments', 'Experiments')}
          </h1>
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
            {msg('evaluations.experimentsDescription', 'Runs for the {workspaceName} workspace.', {
              workspaceName: workspace.workspaceName,
            })}
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          <ButtonLink variant="outline" className="h-9" href={evaluationExperimentCompareHref(workspace.workspaceId)}>
            {msg('evaluations.compare', 'Compare')}
          </ButtonLink>
          <ButtonLink className="h-9" href={evaluationExperimentNewHref(workspace.workspaceId)}>
            <Plus className="size-4" aria-hidden />
            {msg('evaluations.createExperiment', 'New experiment')}
          </ButtonLink>
        </div>
      </header>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
            <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
            <TableHead>{msg('evaluations.agentId', 'Agent')}</TableHead>
            <TableHead>{msg('evaluations.datasetVersion', 'Dataset version')}</TableHead>
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
    return <TableLoadingRow colSpan={4} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={4}
        title={msg('evaluations.experimentsLoadFailed', 'Could not load experiments')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (experiments.length === 0) {
    return <TableEmptyRow colSpan={4}>{msg('evaluations.experimentsEmpty', 'No experiments yet.')}</TableEmptyRow>;
  }
  return experiments.map((experiment) => (
    <TableRow key={experiment.id}>
      <TableCell>
        <a className="font-medium hover:underline" href={evaluationExperimentHref(workspaceId, experiment.id)}>
          {experiment.name}
        </a>
      </TableCell>
      <TableCell>{experiment.status}</TableCell>
      <TableCell>
        {experiment.agent_id} v{experiment.agent_version}
      </TableCell>
      <TableCell>{experiment.version_id}</TableCell>
    </TableRow>
  ));
}
