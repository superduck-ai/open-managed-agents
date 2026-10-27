import { Button } from '../../shared/ui/button';
import { useI18n } from '../../shared/i18n';
import { evaluationErrorMessage } from './api';
import { CopyIdButton } from './CopyIdButton';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import { useNamedRecords } from './EvaluationPickers';
import { ExperimentDetailBody } from './ExperimentDetailBody';
import { caseRecords, datasetRecord, experimentNames, nameMap } from './experimentSummary';
import { evaluationExperimentsHref, useEvaluationCaseList } from './useEvaluationDatasets';
import { useEvaluationEvaluatorList } from './useEvaluationEvaluators';
import { useEvaluationExperiment, useStartEvaluationExperiment } from './useEvaluationExperiments';

const agentsPath = '/v1/agents?beta=true&limit=50';

export function ExperimentDetailPage() {
  const { msg } = useI18n();
  const { workspace, experimentId, query } = useEvaluationExperiment();
  const start = useStartEvaluationExperiment(experimentId);
  const experiment = query.data;
  const datasetId = experiment?.dataset_id ?? '';
  const { datasetQuery, casesQuery } = useEvaluationCaseList(datasetId);
  const evaluators = useEvaluationEvaluatorList();
  const agents = useNamedRecords(agentsPath);
  const names = experiment
    ? experimentNames({
        experiment,
        dataset: datasetRecord(datasetQuery.data),
        evaluators: nameMap(evaluators.query.data),
        cases: caseRecords(casesQuery.data),
        agents: agents.data ?? [],
      })
    : undefined;

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <header className="mb-4">
        <a
          className="text-sm text-muted-foreground hover:text-foreground"
          href={evaluationExperimentsHref(workspace.workspaceId)}
        >
          {msg('evaluations.experiments', 'Experiments')}
        </a>
        <div className="mt-2 flex items-start justify-between gap-6">
          <h1 className="text-2xl font-semibold tracking-tight">{experiment?.name ?? experimentId}</h1>
          {experiment?.status === 'pending' ? (
            <Button
              type="button"
              variant="outline"
              className="h-8"
              disabled={!experimentId || start.isPending}
              onClick={() => start.mutate()}
            >
              {msg('evaluations.start', 'Start')}
            </Button>
          ) : null}
        </div>
        {experiment ? (
          <div className="mt-2 flex items-center gap-2">
            <EvaluationStatusBadge status={experiment.status} />
            <CopyIdButton id={experiment.id} className="w-auto" />
          </div>
        ) : null}
      </header>
      {query.error ? <p className="mb-4 text-sm text-destructive">{evaluationErrorMessage(query.error)}</p> : null}
      {start.error ? <p className="mb-4 text-sm text-destructive">{evaluationErrorMessage(start.error)}</p> : null}
      {experiment && names ? (
        <ExperimentDetailBody
          experiment={experiment}
          names={names}
          cases={caseRecords(casesQuery.data)}
          workspaceId={workspace.workspaceId}
        />
      ) : null}
    </section>
  );
}
