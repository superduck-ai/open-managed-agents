import { Copy, Info, Plus, Scale, Search } from 'lucide-react';
import { useState, type FormEvent } from 'react';

import { cn } from '@/shared/lib/utils';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../shared/ui/tooltip';
import {
  evaluationErrorMessage,
  evaluationVariables,
  type CreateEvaluationEvaluatorInput,
  type EvaluationEvaluator,
  type EvaluationEvaluatorType,
  type EvaluationVariable,
} from './api';
import { CopyIdButton } from './CopyIdButton';
import { EvaluationEmpty } from './EvaluationEmpty';
import { AgentSessionFields, ModelSelect, emptyEvaluatorMount, evaluatorMountPayload } from './EvaluationPickers';
import { EvaluationOriginTabs, type EvaluationOrigin } from './EvaluationOriginTabs';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluatorPresets } from './EvaluatorPresets';
import { evaluationEvaluatorHref } from './useEvaluationDatasets';
import { useCreateEvaluationEvaluator, useEvaluationEvaluatorList } from './useEvaluationEvaluators';

export function EvaluatorsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationEvaluatorList();
  const [createOpen, setCreateOpen] = useState(false);
  const [section, setSection] = useState<EvaluationOrigin>('custom');
  const [search, setSearch] = useState('');
  const evaluators = (query.data?.data ?? []).filter((evaluator) =>
    matchesEvaluatorQuery(search, [evaluator.version.id, evaluator.name, evaluator.desc]),
  );
  const customEmpty =
    section === 'custom' &&
    workspace.workspaceReady &&
    !query.isLoading &&
    !query.error &&
    (query.data?.data.length ?? 0) === 0;
  const description = msg('evaluations.evaluatorsDescription', 'Manage scoring standards and prompt templates.');

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="evaluators" />
      <ResourcePageHeader
        title={msg('evaluations.evaluators', 'Evaluators')}
        description={description}
        actions={
          section === 'custom' ? (
            <Button type="button" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" aria-hidden />
              {msg('evaluations.createEvaluator', 'Create evaluator')}
            </Button>
          ) : null
        }
      />
      <EvaluationOriginTabs kind="evaluator" section={section} onSection={setSection} />
      <div className="relative mb-3 max-w-sm">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={msg('evaluations.searchEvaluators', 'Search evaluators...')}
          aria-label={msg('evaluations.searchEvaluators', 'Search evaluators...')}
          className="h-9 bg-background pl-8"
        />
      </div>
      {section === 'presets' ? (
        <EvaluatorPresets search={search} onCreated={() => setSection('custom')} />
      ) : customEmpty ? (
        <EvaluationEmpty
          icon={<Scale aria-hidden />}
          title={msg('evaluations.evaluatorsEmpty', 'No evaluators yet.')}
          description={description}
          action={
            <Button type="button" variant="outline" onClick={() => setCreateOpen(true)}>
              {msg('evaluations.createEvaluator', 'Create evaluator')}
            </Button>
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
                {msg('evaluations.descriptionField', 'Description')}
              </TableHead>
              <TableHead className="w-24 text-xs font-normal text-muted-foreground">
                {msg('evaluations.evaluatorType', 'Type')}
              </TableHead>
              <TableHead className="w-[22%] text-xs font-normal text-muted-foreground">
                {msg('evaluations.insertVariables', 'Insert variables')}
              </TableHead>
              <TableHead className="w-28 text-xs font-normal text-muted-foreground">
                {msg('evaluations.createdAt', 'Created')}
              </TableHead>
              <TableHead className="w-20 text-right text-xs font-normal text-muted-foreground">
                {msg('evaluations.actions', 'Action')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <EvaluatorRows
              evaluators={evaluators}
              workspaceId={workspace.workspaceId}
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
  workspaceId,
  isLoading,
  errorMessage,
  onRetry,
}: {
  evaluators: EvaluationEvaluator[];
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
        title={msg('evaluations.evaluatorsLoadFailed', 'Could not load evaluators')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (evaluators.length === 0) {
    return <TableEmptyRow colSpan={7}>{msg('evaluations.evaluatorsEmpty', 'No evaluators yet.')}</TableEmptyRow>;
  }
  return evaluators.map((evaluator) => (
    <TableRow key={evaluator.id}>
      <TableCell>
        <CopyIdButton id={evaluator.version.id} />
      </TableCell>
      <TableCell className="font-medium">
        <a className="block truncate" href={evaluationEvaluatorHref(workspaceId, evaluator.id)}>
          {evaluator.name}
        </a>
      </TableCell>
      <TableCell className="text-muted-foreground">
        <span className="block truncate">{evaluator.desc}</span>
      </TableCell>
      <TableCell>{evaluatorTypeLabel(evaluator.version.evaluator_type, msg)}</TableCell>
      <TableCell>
        <VariableMarks names={evaluator.version.variables} />
      </TableCell>
      <TableCell className="text-muted-foreground">{evaluationDateLabel(evaluator.created_at)}</TableCell>
      <TableCell className="text-right">
        <a
          className="text-sm text-muted-foreground hover:text-foreground"
          href={evaluationEvaluatorHref(workspaceId, evaluator.id)}
        >
          {msg('evaluations.edit', 'Edit')}
        </a>
      </TableCell>
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
  const [environmentId, setEnvironmentId] = useState('');
  const [mounts, setMounts] = useState([emptyEvaluatorMount()]);
  const starter = promptStarter(msg);
  const [prompt, setPrompt] = useState(starter);
  const [variables, setVariables] = useState<string[]>([]);

  function submit(event: FormEvent) {
    event.preventDefault();
    const input: CreateEvaluationEvaluatorInput = {
      name,
      desc,
      evaluator_type: evaluatorType,
      prompt_template: prompt,
      variables,
    };
    if (evaluatorType === 'llm') {
      input.model = model;
    } else {
      input.agent_id = agentId;
      if (environmentId) {
        input.environment_id = environmentId;
      }
      const resources = evaluatorMountPayload(mounts);
      if (resources) {
        input.resources = resources;
      }
    }
    create.mutate(input, {
      onSuccess: () => {
        setName('');
        setDesc('');
        setModel('');
        setAgentId('');
        setEnvironmentId('');
        setMounts([emptyEvaluatorMount()]);
        setPrompt(promptStarter(msg));
        setVariables([]);
        setEvaluatorType('llm');
        onOpenChange(false);
      },
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(44rem,calc(100vh-4rem))] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{msg('evaluations.createEvaluator', 'Create evaluator')}</DialogTitle>
        </DialogHeader>
        <form className="grid min-w-0 gap-5" onSubmit={submit}>
          <div className="grid gap-1.5">
            <FieldLabel htmlFor="evaluation-evaluator-name" required>
              {msg('evaluations.name', 'Name')}
            </FieldLabel>
            <Input
              id="evaluation-evaluator-name"
              value={name}
              maxLength={50}
              placeholder={msg('evaluations.namePlaceholder', 'For example: Answer consistency')}
              onChange={(event) => setName(event.target.value.slice(0, 50))}
              required
            />
            <p className="text-xs text-muted-foreground">{name.length}/50</p>
          </div>
          <div className="grid gap-1.5">
            <FieldLabel htmlFor="evaluation-evaluator-description">
              {msg('evaluations.descriptionField', 'Description')}
            </FieldLabel>
            <Input
              id="evaluation-evaluator-description"
              value={desc}
              placeholder={msg('evaluations.evaluatorDescPlaceholder', 'What does this evaluator judge?')}
              onChange={(event) => setDesc(event.target.value)}
            />
          </div>
          <EvaluatorTypeField value={evaluatorType} onChange={setEvaluatorType} />
          {evaluatorType === 'llm' ? (
            <ModelSelect value={model} onChange={setModel} />
          ) : (
            <AgentSessionFields
              agentId={agentId}
              environmentId={environmentId}
              mounts={mounts}
              onAgentId={setAgentId}
              onEnvironmentId={setEnvironmentId}
              onMounts={setMounts}
            />
          )}
          <VariableChips
            value={variables}
            onChange={(next) => {
              setPrompt(syncPromptVariables(variables, next, prompt));
              setVariables(next);
            }}
          />
          <div className="grid gap-1.5">
            <FieldLabel htmlFor="evaluation-evaluator-prompt" required>
              {msg('evaluations.scoringPrompt', 'Prompt')}
            </FieldLabel>
            <Textarea
              id="evaluation-evaluator-prompt"
              className="min-h-44 text-sm"
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              required
            />
          </div>
          <OutputContract />
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button
              type="submit"
              disabled={create.isPending || !evaluatorReady(evaluatorType, model, agentId, prompt, variables)}
            >
              {msg('evaluations.dialogCreate', 'Create evaluator')}
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
    <div className="grid gap-1.5">
      <FieldLabel htmlFor="evaluation-evaluator-category">{msg('evaluations.category', 'Category')}</FieldLabel>
      <select
        id="evaluation-evaluator-category"
        className="h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
        value={value}
        onChange={(event) => onChange(event.target.value === 'agent' ? 'agent' : 'llm')}
      >
        <option value="llm">{msg('evaluations.categoryLLM', 'LLM evaluator')}</option>
        <option value="agent">{msg('evaluations.categoryAgent', 'Agent evaluator')}</option>
      </select>
      <p className="text-sm text-muted-foreground">
        {value === 'llm'
          ? msg('evaluations.llmCategoryHint', 'One model call. Fast and inexpensive.')
          : msg('evaluations.agentCategoryHint', 'Scores inside a sandbox session and can read the run artifacts.')}
      </p>
    </div>
  );
}

function evaluatorReady(
  evaluatorType: EvaluationEvaluatorType,
  model: string,
  agentId: string,
  prompt: string,
  variables: string[],
) {
  if (evaluatorType === 'llm' && model.length === 0) {
    return false;
  }
  if (evaluatorType === 'agent' && agentId.length === 0) {
    return false;
  }
  return prompt.trim().length > 0 && variables.length > 0;
}

function VariableChips({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-2">
      <span className="text-sm font-medium">{msg('evaluations.insertVariables', 'Insert variables')}</span>
      <TooltipProvider>
        <div className="flex flex-wrap gap-2" role="group" aria-label={msg('evaluations.variables', 'Variables')}>
          {evaluationVariables.map((name) => {
            const selected = value.includes(name);
            return (
              <Tooltip key={name}>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      aria-label={name}
                      aria-pressed={selected}
                      className={cn(
                        'rounded-md bg-muted px-2.5 py-1 text-sm text-foreground hover:bg-muted/80',
                        selected && 'bg-foreground/10',
                      )}
                      onClick={() => onChange(selected ? value.filter((item) => item !== name) : [...value, name])}
                    />
                  }
                >
                  {name}
                </TooltipTrigger>
                <TooltipContent className="max-w-sm">{variableHint(name, msg)}</TooltipContent>
              </Tooltip>
            );
          })}
        </div>
      </TooltipProvider>
    </div>
  );
}

const scoreSchema = `{
  "score": 0.85,
  "explanation": "...",
  "details": {}
}`;

function OutputContract() {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex items-center gap-1.5 text-sm font-medium">
          {msg('evaluations.outputMetricsReadonly', 'Output metrics (read-only)')}
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    className="text-muted-foreground"
                    aria-label={msg(
                      'evaluations.outputMetricsHint',
                      'The evaluator must return score, explanation, and details.',
                    )}
                  />
                }
              >
                <Info className="size-3.5" aria-hidden />
              </TooltipTrigger>
              <TooltipContent className="max-w-sm">
                {msg('evaluations.outputMetricsHint', 'The evaluator must return score, explanation, and details.')}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-muted-foreground"
          onClick={() => {
            const write = navigator.clipboard?.writeText(scoreSchema);
            if (!write) {
              return;
            }
            void write.then(() => {
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1200);
            });
          }}
        >
          <Copy className="size-3.5" aria-hidden />
          {copied ? msg('evaluations.copied', 'Copied') : msg('evaluations.copySchema', 'Copy')}
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-md bg-muted px-4 py-3 font-mono text-sm text-muted-foreground">
        {scoreSchema}
      </pre>
    </div>
  );
}

function FieldLabel({
  htmlFor,
  required = false,
  children,
}: {
  htmlFor: string;
  required?: boolean;
  children: string;
}) {
  return (
    <div className="flex items-center gap-0.5">
      {required ? (
        <span className="text-destructive" aria-hidden>
          *
        </span>
      ) : null}
      <Label htmlFor={htmlFor}>{children}</Label>
    </div>
  );
}

function promptStarter(msg: ReturnType<typeof useI18n>['msg']) {
  return msg(
    'evaluations.promptStarter',
    '## Scoring\n1. Accuracy: Does the answer solve the question?\n2. Completeness: Are the key points covered?\n3. Clarity: Is the answer structured and easy to understand?\n\n## Requirements\n- Judge the Agent answer against each checkpoint.\n- Give a score and a short reason across these dimensions.',
  );
}

function variableHint(name: EvaluationVariable, msg: ReturnType<typeof useI18n>['msg']) {
  if (name === 'input') {
    return msg('evaluations.varInput', 'The question sent to the Agent. Multi-turn dialogue is joined in turn order.');
  }
  if (name === 'output') {
    return msg(
      'evaluations.varOutput',
      "The Agent's last non-empty answer. Earlier turns are in the trajectory, and generated files are excluded.",
    );
  }
  if (name === 'checkpoints') {
    return msg(
      'evaluations.varCheckpoints',
      'The checks set on the case, covering the answer, the run, and the overall ask. Empty when unset.',
    );
  }
  if (name === 'agent_trajectory') {
    return msg(
      'evaluations.varTrajectory',
      'Session events in time order, including status, user messages, thinking, model requests, tool calls, replies, and usage.',
    );
  }
  return msg(
    'evaluations.varInvocation',
    "The Agent's invocation result, including the answer, trace, final status, errors, and run metrics.",
  );
}

function VariableMarks({ names }: { names: string[] }) {
  if (names.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <span className="flex flex-wrap gap-1">
      {names.map((name) => (
        <span key={name} className="rounded-md border px-1.5 py-0.5 text-xs text-muted-foreground">
          {name}
        </span>
      ))}
    </span>
  );
}

function evaluatorTypeLabel(type: EvaluationEvaluatorType, msg: ReturnType<typeof useI18n>['msg']) {
  if (type === 'llm') {
    return msg('evaluations.evaluatorLLM', 'LLM');
  }
  return msg('evaluations.evaluatorAgent', 'Agent');
}

function matchesEvaluatorQuery(query: string, parts: string[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.some((part) => part.toLowerCase().includes(needle));
}

function evaluationDateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
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
