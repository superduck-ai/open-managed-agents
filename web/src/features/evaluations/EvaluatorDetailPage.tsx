import { useI18n } from '../../shared/i18n';
import { evaluationErrorMessage, type EvaluationEvaluator } from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluatorVersionForm } from './EvaluatorVersionForm';
import { agentDetailHref } from '../managed-agents/utils';
import { evaluationEvaluatorsHref, useEvaluationWorkspace } from './useEvaluationDatasets';
import { evaluatorIdFromPath, useEvaluationEvaluator } from './useEvaluationEvaluators';

const scoreSchema = `{
  "score": 0,
  "explanation": "",
  "details": {}
}`;

export function EvaluatorDetailPage() {
  const { msg } = useI18n();
  const workspace = useEvaluationWorkspace();
  const evaluatorId = evaluatorIdFromPath();
  const { query } = useEvaluationEvaluator(evaluatorId);
  const evaluator = query.data;

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="evaluators" />
      <header className="mb-5">
        <a
          className="text-sm text-muted-foreground underline-offset-4 hover:underline"
          href={evaluationEvaluatorsHref(workspace.workspaceId)}
        >
          {msg('evaluations.evaluators', 'Evaluators')}
        </a>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight">{evaluator?.name ?? evaluatorId}</h1>
        {evaluator ? (
          <p className="mt-1 text-sm text-muted-foreground">
            {evaluator.version.evaluator_type} · v{evaluator.version.version}
          </p>
        ) : null}
      </header>
      {query.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(query.error)}</p> : null}
      {evaluator ? <EvaluatorBlocks evaluator={evaluator} workspaceId={workspace.workspaceId} /> : null}
    </section>
  );
}

function EvaluatorBlocks({ evaluator, workspaceId }: { evaluator: EvaluationEvaluator; workspaceId: string }) {
  const { msg } = useI18n();
  const agent = evaluator.version.evaluator_type === 'agent';
  return (
    <div className="grid max-w-3xl gap-8">
      <section className="grid gap-2">
        <h2 className="text-sm font-medium">{msg('evaluations.promptBlock', 'Prompt template')}</h2>
        <EvaluatorVersionForm evaluator={evaluator} />
      </section>
      <section className="grid gap-2">
        <h2 className="text-sm font-medium">{msg('evaluations.outputMetrics', 'Output metrics')}</h2>
        <pre className="whitespace-pre-wrap text-sm text-muted-foreground">{scoreSchema}</pre>
        {evaluator.version.agent_contract ? (
          <p className="text-sm text-muted-foreground">
            {evaluator.version.agent_contract.result_path} → {evaluator.version.agent_contract.score_path}
          </p>
        ) : null}
      </section>
      <section className="grid gap-2">
        <h2 className="text-sm font-medium">{msg('evaluations.mountedSkills', 'Mounted skills')}</h2>
        <p className="text-sm text-muted-foreground">
          {agent
            ? msg(
                'evaluations.skillsOnAgent',
                'Skills stay on the bound Agent. This evaluator does not mount skills of its own.',
              )
            : msg(
                'evaluations.skillsUnused',
                'An LLM evaluator does not mount skills. Scoring uses the prompt template.',
              )}
        </p>
        {agent && evaluator.version.agent_id ? (
          <a
            className="text-sm font-medium underline-offset-4 hover:underline"
            href={agentDetailHref(workspaceId, evaluator.version.agent_id)}
          >
            {msg('evaluations.openAgent', 'Open Agent')} {evaluator.version.agent_id}
          </a>
        ) : null}
      </section>
    </div>
  );
}
