import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage } from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { useEvaluationExperiment, useStartEvaluationExperiment } from './useEvaluationExperiments';

export function ExperimentDetailPage() {
  const { msg } = useI18n();
  const { workspace, experimentId, query } = useEvaluationExperiment();
  const start = useStartEvaluationExperiment(experimentId);
  const experiment = query.data;

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <header className="mb-5 flex items-start justify-between gap-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{experiment?.name ?? experimentId}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{experiment?.status}</p>
        </div>
        <Button
          type="button"
          className="h-9"
          disabled={!experimentId || start.isPending}
          onClick={() => start.mutate()}
        >
          {msg('evaluations.start', 'Start')}
        </Button>
      </header>
      {query.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(query.error)}</p> : null}
      {experiment ? (
        <div className="mb-6 grid gap-1 text-sm">
          <span>
            {msg('evaluations.agentId', 'Agent')}: {experiment.agent_id} v{experiment.agent_version}
          </span>
          <span>
            {msg('evaluations.environmentId', 'Environment')}: {experiment.environment_id}
          </span>
          <span>
            {msg('evaluations.datasetVersion', 'Dataset version')}: {experiment.version_id}
          </span>
        </div>
      ) : null}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.cases', 'Cases')}</TableHead>
            <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
            <TableHead>{msg('evaluations.session', 'Session')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(experiment?.items ?? []).map((item) => (
            <TableRow key={item.id}>
              <TableCell>{item.case_id}</TableCell>
              <TableCell>{item.error || item.status}</TableCell>
              <TableCell>
                {item.session_id ? (
                  <a
                    className="hover:underline"
                    href={`/workspaces/${encodeURIComponent(workspace.workspaceId)}/sessions/${encodeURIComponent(item.session_id)}`}
                  >
                    {item.session_id}
                  </a>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  );
}
