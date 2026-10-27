import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { evaluationErrorMessage, type EvaluationEvaluator, type EvaluationEvaluatorPreset } from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluatorVersionForm } from './EvaluatorVersionForm';
import { agentDetailHref } from '../managed-agents/utils';
import { evaluationEvaluatorsHref, useEvaluationWorkspace } from './useEvaluationDatasets';
import { evaluatorIdFromPath, useEvaluationEvaluator, useEvaluationEvaluatorPreset } from './useEvaluationEvaluators';

const scoreSchema = `{
  "score": 0,
  "explanation": "",
  "details": {}
}`;

type EvaluatorView = EvaluationEvaluator | EvaluationEvaluatorPreset;

export function EvaluatorDetailPage() {
  const { msg } = useI18n();
  const workspace = useEvaluationWorkspace();
  const evaluatorId = evaluatorIdFromPath();
  const presetView = new URLSearchParams(window.location.search).get('preset') === '1';
  const custom = useEvaluationEvaluator(presetView ? '' : evaluatorId);
  const preset = useEvaluationEvaluatorPreset(evaluatorId, presetView);
  const query = presetView ? preset.query : custom.query;
  const evaluator = query.data;
  const [editing, setEditing] = useState(false);

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="evaluators" />
      <header className="mb-4 flex items-start justify-between gap-6">
        <div className="min-w-0">
          <a
            className="text-sm text-muted-foreground hover:text-foreground"
            href={evaluationEvaluatorsHref(workspace.workspaceId)}
          >
            {msg('evaluations.evaluators', 'Evaluators')}
          </a>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">{evaluator?.name ?? evaluatorId}</h1>
          {evaluator ? <EvaluatorMarks evaluator={evaluator} readOnly={presetView} /> : null}
        </div>
        {evaluator && !presetView ? (
          <Button
            type="button"
            variant="outline"
            className="h-8 shrink-0"
            onClick={() => setEditing((value) => !value)}
          >
            {msg('evaluations.edit', 'Edit')}
          </Button>
        ) : null}
      </header>
      {query.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(query.error)}</p> : null}
      {evaluator ? (
        <EvaluatorBlocks evaluator={evaluator} workspaceId={workspace.workspaceId} editing={editing && !presetView} />
      ) : null}
    </section>
  );
}

function EvaluatorMarks({ evaluator, readOnly }: { evaluator: EvaluatorView; readOnly: boolean }) {
  const { msg } = useI18n();
  return (
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <OriginBadge readOnly={readOnly} />
      <GrayBadge>{evaluatorTypeLabel(evaluator, msg)}</GrayBadge>
      {readOnly ? (
        <span className="inline-flex h-5 items-center rounded-md border border-[var(--success)]/30 bg-[var(--success-bg)] px-1.5 text-xs font-medium text-[var(--success)]">
          {msg('evaluations.readOnly', 'Read-only')}
        </span>
      ) : null}
      <span className="font-mono text-xs text-muted-foreground">{evaluator.version.id}</span>
    </div>
  );
}

function OriginBadge({ readOnly }: { readOnly: boolean }) {
  const { msg } = useI18n();
  if (readOnly) {
    return <GrayBadge>{msg('evaluations.builtinBadge', 'Built-in')}</GrayBadge>;
  }
  return (
    <span className="inline-flex h-5 items-center rounded-md border border-[var(--success)]/30 bg-[var(--success-bg)] px-1.5 text-xs font-medium text-[var(--success)]">
      {msg('evaluations.customBadge', 'Custom')}
    </span>
  );
}

function GrayBadge({ children }: { children: string }) {
  return (
    <span className="inline-flex h-5 items-center rounded-md border bg-muted px-1.5 text-xs text-muted-foreground">
      {children}
    </span>
  );
}

function EvaluatorBlocks({
  evaluator,
  workspaceId,
  editing,
}: {
  evaluator: EvaluatorView;
  workspaceId: string;
  editing: boolean;
}) {
  return (
    <div className="flex flex-col gap-8">
      <BasicInfo evaluator={evaluator} />
      <PromptBlock evaluator={evaluator} editing={editing} />
      <OutputBlock evaluator={evaluator} />
      <SkillsBlock evaluator={evaluator} workspaceId={workspaceId} />
    </div>
  );
}

function BasicInfo({ evaluator }: { evaluator: EvaluatorView }) {
  const { msg } = useI18n();
  return (
    <section className="rounded-lg border px-5 py-5">
      <h2 className="text-sm font-medium">{msg('evaluations.basicInfo', 'Basic info')}</h2>
      <dl className="mt-4 grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-x-8 gap-y-4 text-sm">
        <dt className="text-muted-foreground">{msg('evaluations.name', 'Name')}</dt>
        <dd>{evaluator.name}</dd>
        <dt className="text-muted-foreground">{msg('evaluations.evaluatorType', 'Type')}</dt>
        <dd>
          <GrayBadge>{evaluatorTypeLabel(evaluator, msg)}</GrayBadge>
        </dd>
        <dt className="text-muted-foreground">{msg('evaluations.descriptionField', 'Description')}</dt>
        <dd>{evaluator.desc || '—'}</dd>
        <dt className="text-muted-foreground">{msg('evaluations.systemVariables', 'System variables')}</dt>
        <dd>
          <VariableChips names={evaluator.version.variables} />
        </dd>
      </dl>
    </section>
  );
}

function PromptBlock({ evaluator, editing }: { evaluator: EvaluatorView; editing: boolean }) {
  const { msg } = useI18n();
  const agent = evaluator.version.evaluator_type === 'agent';
  return (
    <section className="rounded-lg border px-5 py-5">
      <h2 className="text-sm font-medium">{msg('evaluations.promptBlock', 'Prompt template')}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {msg('evaluations.promptUsedFor', 'Prompt used to score each case.')}
      </p>
      {editing && evaluator.type === 'evaluation_evaluator' ? (
        <div className="mt-4">
          <EvaluatorVersionForm evaluator={evaluator} />
        </div>
      ) : (
        <pre className="mt-4 whitespace-pre-wrap rounded-md bg-muted px-4 py-3 text-sm text-muted-foreground">
          {evaluator.version.prompt_template ||
            (agent
              ? msg(
                  'evaluations.agentContract',
                  'Reads /data/session/result.json and artifacts, and writes /output/score.json with score, explanation, and details.',
                )
              : '')}
        </pre>
      )}
    </section>
  );
}

function OutputBlock({ evaluator }: { evaluator: EvaluatorView }) {
  const { msg } = useI18n();
  const contract = evaluator.version.agent_contract;
  return (
    <section className="rounded-lg border px-5 py-5">
      <h2 className="text-sm font-medium">{msg('evaluations.outputMetrics', 'Output metrics')}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {msg('evaluations.outputMustReturn', 'Structured output this evaluator must return.')}
      </p>
      <pre className="mt-4 whitespace-pre-wrap rounded-md bg-muted px-4 py-3 text-sm text-muted-foreground">
        {scoreSchema}
      </pre>
      {contract ? (
        <p className="mt-3 text-sm text-muted-foreground">
          {contract.result_path} → {contract.score_path}
        </p>
      ) : null}
    </section>
  );
}

function SkillsBlock({ evaluator, workspaceId }: { evaluator: EvaluatorView; workspaceId: string }) {
  const { msg } = useI18n();
  const agent = evaluator.version.evaluator_type === 'agent';
  return (
    <section className="rounded-lg border px-5 py-5">
      <h2 className="text-sm font-medium">{msg('evaluations.skillDependencies', 'Skill dependencies')}</h2>
      <p className="mt-3 text-sm text-muted-foreground">
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
          className="mt-3 inline-flex rounded-md bg-muted px-2 py-1 text-sm hover:text-foreground"
          href={agentDetailHref(workspaceId, evaluator.version.agent_id)}
        >
          {msg('evaluations.openAgent', 'Open Agent')} {evaluator.version.agent_id}
        </a>
      ) : null}
      {agent && evaluator.version.environment_id ? (
        <p className="mt-3 text-sm">
          {msg('evaluations.environmentOptional', 'Environment (optional)')}: {evaluator.version.environment_id}
        </p>
      ) : null}
      {agent && evaluator.version.resources?.length ? (
        <ul className="mt-3 grid gap-1 text-sm">
          {evaluator.version.resources.map((file) => (
            <li key={`${file.file_id}:${file.path}`}>
              {file.file_id} → {file.path}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function VariableChips({ names }: { names: string[] }) {
  if (names.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <span className="flex flex-wrap gap-2">
      {names.map((name) => (
        <span key={name} className="rounded-md bg-muted px-2 py-0.5 text-xs">
          {name}
        </span>
      ))}
    </span>
  );
}

function evaluatorTypeLabel(evaluator: EvaluatorView, msg: ReturnType<typeof useI18n>['msg']) {
  if (evaluator.version.evaluator_type === 'llm') {
    return msg('evaluations.evaluatorLLM', 'LLM');
  }
  return msg('evaluations.evaluatorAgent', 'Agent');
}
