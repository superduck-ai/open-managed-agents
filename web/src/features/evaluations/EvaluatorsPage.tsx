import { Plus } from 'lucide-react';
import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '../../shared/ui/toggle-group';
import {
  evaluationErrorMessage,
  evaluationVariables,
  type CreateEvaluationEvaluatorInput,
  type EvaluationEvaluator,
  type EvaluationEvaluatorType,
} from './api';
import { EvaluationOriginTabs, type EvaluationOrigin } from './EvaluationOriginTabs';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluatorPresets } from './EvaluatorPresets';
import { useCreateEvaluationEvaluator, useEvaluationEvaluatorList } from './useEvaluationEvaluators';

export function EvaluatorsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationEvaluatorList();
  const [createOpen, setCreateOpen] = useState(false);
  const [section, setSection] = useState<EvaluationOrigin>('custom');
  const evaluators = query.data?.data ?? [];

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="evaluators" />
      <header className="mb-5 flex items-start justify-between gap-6">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            {msg('evaluations.evaluators', 'Evaluators')}
          </h1>
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
            {msg('evaluations.evaluatorsDescription', 'Custom scoring for the {workspaceName} workspace.', {
              workspaceName: workspace.workspaceName,
            })}
          </p>
        </div>
        {section === 'custom' ? (
          <Button type="button" className="h-9 shrink-0" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden />
            {msg('evaluations.createEvaluator', 'Create evaluator')}
          </Button>
        ) : null}
      </header>
      <EvaluationOriginTabs section={section} onSection={setSection} />
      {section === 'presets' ? (
        <EvaluatorPresets onCreated={() => setSection('custom')} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
              <TableHead>{msg('evaluations.evaluatorType', 'Type')}</TableHead>
              <TableHead>{msg('evaluations.descriptionField', 'Description')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <EvaluatorRows
              evaluators={evaluators}
              isLoading={!workspace.workspaceReady || query.isLoading}
              errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
              onRetry={() => void query.refetch()}
            />
          </TableBody>
        </Table>
      )}
      <CreateEvaluatorDialog open={createOpen} onOpenChange={setCreateOpen} />
    </section>
  );
}

function EvaluatorRows({
  evaluators,
  isLoading,
  errorMessage,
  onRetry,
}: {
  evaluators: EvaluationEvaluator[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={3} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={3}
        title={msg('evaluations.evaluatorsLoadFailed', 'Could not load evaluators')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (evaluators.length === 0) {
    return <TableEmptyRow colSpan={3}>{msg('evaluations.evaluatorsEmpty', 'No evaluators yet.')}</TableEmptyRow>;
  }
  return evaluators.map((evaluator) => (
    <TableRow key={evaluator.id}>
      <TableCell className="font-medium">{evaluator.name}</TableCell>
      <TableCell>{evaluator.version.evaluator_type}</TableCell>
      <TableCell className="text-muted-foreground">{evaluator.desc}</TableCell>
    </TableRow>
  ));
}

function CreateEvaluatorDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const create = useCreateEvaluationEvaluator();
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [evaluatorType, setEvaluatorType] = useState<EvaluationEvaluatorType>('llm');
  const [model, setModel] = useState('');
  const [agentId, setAgentId] = useState('');
  const [prompt, setPrompt] = useState('');
  const [variables, setVariables] = useState<string[]>([]);

  function submit(event: FormEvent) {
    event.preventDefault();
    const input: CreateEvaluationEvaluatorInput = { name, desc, evaluator_type: evaluatorType, variables };
    if (evaluatorType === 'llm') {
      input.model = model;
      input.prompt_template = prompt;
    } else {
      input.agent_id = agentId;
    }
    create.mutate(input, {
      onSuccess: () => {
        setName('');
        setDesc('');
        setModel('');
        setAgentId('');
        setPrompt('');
        setVariables([]);
        setEvaluatorType('llm');
        onOpenChange(false);
      },
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{msg('evaluations.createEvaluator', 'Create evaluator')}</DialogTitle>
        </DialogHeader>
        <form className="grid gap-3" onSubmit={submit}>
          <Label htmlFor="evaluation-evaluator-name">{msg('evaluations.name', 'Name')}</Label>
          <Input
            id="evaluation-evaluator-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            required
          />
          <Label htmlFor="evaluation-evaluator-description">{msg('evaluations.descriptionField', 'Description')}</Label>
          <Input id="evaluation-evaluator-description" value={desc} onChange={(event) => setDesc(event.target.value)} />
          <EvaluatorTypeField value={evaluatorType} onChange={setEvaluatorType} />
          {evaluatorType === 'llm' ? (
            <LLMEvaluatorFields model={model} prompt={prompt} onModel={setModel} onPrompt={setPrompt} />
          ) : (
            <AgentEvaluatorFields agentId={agentId} onAgentId={setAgentId} />
          )}
          <VariableChips
            value={variables}
            onChange={(next) => {
              if (evaluatorType === 'llm') {
                setPrompt((current) => syncPromptVariables(variables, next, current));
              }
              setVariables(next);
            }}
          />
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <DialogFooter>
            <Button type="submit" disabled={create.isPending}>
              {msg('evaluations.createEvaluator', 'Create evaluator')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function EvaluatorTypeField({
  value,
  onChange,
}: {
  value: EvaluationEvaluatorType;
  onChange: (value: EvaluationEvaluatorType) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <span className="text-sm font-medium">{msg('evaluations.evaluatorType', 'Type')}</span>
      <ToggleGroup
        aria-label={msg('evaluations.evaluatorType', 'Type')}
        value={[value]}
        onValueChange={(next) => {
          if (next[0] === 'llm' || next[0] === 'agent') {
            onChange(next[0]);
          }
        }}
      >
        <ToggleGroupItem value="llm">{msg('evaluations.evaluatorLLM', 'LLM')}</ToggleGroupItem>
        <ToggleGroupItem value="agent">{msg('evaluations.evaluatorAgent', 'Agent')}</ToggleGroupItem>
      </ToggleGroup>
    </>
  );
}

function LLMEvaluatorFields({
  model,
  prompt,
  onModel,
  onPrompt,
}: {
  model: string;
  prompt: string;
  onModel: (value: string) => void;
  onPrompt: (value: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <Label htmlFor="evaluation-evaluator-model">{msg('evaluations.model', 'Model')}</Label>
      <Input id="evaluation-evaluator-model" value={model} onChange={(event) => onModel(event.target.value)} required />
      <Label htmlFor="evaluation-evaluator-prompt">{msg('evaluations.promptTemplate', 'Prompt')}</Label>
      <Textarea
        id="evaluation-evaluator-prompt"
        value={prompt}
        onChange={(event) => onPrompt(event.target.value)}
        required
      />
    </>
  );
}

function AgentEvaluatorFields({ agentId, onAgentId }: { agentId: string; onAgentId: (value: string) => void }) {
  const { msg } = useI18n();
  return (
    <>
      <Label htmlFor="evaluation-evaluator-agent">{msg('evaluations.agentId', 'Agent')}</Label>
      <Input
        id="evaluation-evaluator-agent"
        value={agentId}
        onChange={(event) => onAgentId(event.target.value)}
        required
      />
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.agentContract',
          'Reads /data/session/result.json and artifacts, and writes /output/score.json with score, explanation, and details.',
        )}
      </p>
    </>
  );
}

function VariableChips({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
  const { msg } = useI18n();
  return (
    <>
      <span className="text-sm font-medium">{msg('evaluations.variables', 'Variables')}</span>
      <ToggleGroup
        multiple
        aria-label={msg('evaluations.variables', 'Variables')}
        value={value}
        onValueChange={onChange}
      >
        {evaluationVariables.map((name) => (
          <ToggleGroupItem key={name} value={name}>
            {name}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </>
  );
}

function syncPromptVariables(previous: string[], next: string[], prompt: string) {
  let updated = prompt;
  for (const name of previous) {
    if (!next.includes(name)) {
      updated = updated.replaceAll(`{{${name}}}`, '');
    }
  }
  for (const name of next) {
    const token = `{{${name}}}`;
    if (!previous.includes(name) && !updated.includes(token)) {
      updated = updated.trim() ? `${updated.trim()} ${token}` : token;
    }
  }
  return updated;
}
