import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '../../shared/ui/toggle-group';
import {
  evaluationErrorMessage,
  evaluationVariables,
  type CreateEvaluationEvaluatorVersionInput,
  type EvaluationEvaluator,
} from './api';
import { useCreateEvaluationEvaluatorVersion } from './useEvaluationEvaluators';

export function EvaluatorVersionForm({ evaluator }: { evaluator: EvaluationEvaluator }) {
  const agent = evaluator.version.evaluator_type === 'agent';
  return <EvaluatorVersionFields key={evaluator.version.id} evaluator={evaluator} agent={agent} />;
}

function EvaluatorVersionFields({ evaluator, agent }: { evaluator: EvaluationEvaluator; agent: boolean }) {
  const { msg } = useI18n();
  const save = useCreateEvaluationEvaluatorVersion(evaluator.id);
  const [model, setModel] = useState(evaluator.version.model ?? '');
  const [prompt, setPrompt] = useState(evaluator.version.prompt_template ?? '');
  const [variables, setVariables] = useState<string[]>(evaluator.version.variables);
  const [agentId, setAgentId] = useState(evaluator.version.agent_id ?? '');

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    const input: CreateEvaluationEvaluatorVersionInput = agent
      ? { agent_id: agentId }
      : { model, prompt_template: prompt, variables };
    await save.mutateAsync(input);
  }

  return (
    <form className="grid gap-3" onSubmit={(event) => void onSubmit(event)}>
      {agent ? (
        <>
          <Label htmlFor="evaluation-version-agent">{msg('evaluations.agentId', 'Agent')}</Label>
          <Input
            id="evaluation-version-agent"
            value={agentId}
            onChange={(event) => setAgentId(event.target.value)}
            required
          />
          <p className="whitespace-pre-wrap text-sm text-muted-foreground">
            {evaluator.version.prompt_template ||
              msg(
                'evaluations.agentContract',
                'Reads /data/session/result.json and artifacts, and writes /output/score.json with score, explanation, and details.',
              )}
          </p>
          <p className="text-sm text-muted-foreground">
            {msg(
              'evaluations.agentPromptFixed',
              'An agent evaluator does not edit its prompt. Saving a new version updates the bound Agent and copies the current prompt.',
            )}
          </p>
        </>
      ) : (
        <>
          <Label htmlFor="evaluation-version-model">{msg('evaluations.model', 'Model')}</Label>
          <Input
            id="evaluation-version-model"
            value={model}
            onChange={(event) => setModel(event.target.value)}
            required
          />
          <Label htmlFor="evaluation-version-prompt">{msg('evaluations.promptTemplate', 'Prompt')}</Label>
          <Textarea
            id="evaluation-version-prompt"
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            required
          />
          <span className="text-sm font-medium">{msg('evaluations.variables', 'Variables')}</span>
          <ToggleGroup
            multiple
            aria-label={msg('evaluations.variables', 'Variables')}
            value={variables}
            onValueChange={setVariables}
          >
            {evaluationVariables.map((name) => (
              <ToggleGroupItem key={name} value={name}>
                {name}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <p className="text-sm text-muted-foreground">
            {msg(
              'evaluations.promptReadOnly',
              'Saving creates a new version. Experiments already created keep the previous version.',
            )}
          </p>
        </>
      )}
      {save.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(save.error)}</p> : null}
      <Button type="submit" disabled={save.isPending} className="w-fit">
        {msg('evaluations.saveVersion', 'Save as new version')}
      </Button>
    </form>
  );
}
