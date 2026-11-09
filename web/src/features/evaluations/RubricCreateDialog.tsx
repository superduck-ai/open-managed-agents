import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent, type ReactNode } from 'react';

import { listAgents } from '../managed-agents/api';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { Textarea } from '../../shared/ui/textarea';
import { createEvaluationRubric, evaluationError, generateEvaluationRubric, listEvaluationDatasets } from './api';
import { useEvaluationScope } from './useEvaluationScope';

type RubricSource = 'generate' | 'write';
type SampleKind = 'dataset' | 'positive' | 'negative';

const rubricTemplate = `# 评分标准

## 正确性
- 5分：完全正确，关键事实齐全
- 3分：方向正确但有遗漏或小错
- 1分：存在实质性错误

## 完整性
- 5分：覆盖全部要点`;

export function RubricCreateDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [source, setSource] = useState<RubricSource>('generate');
  const [agentId, setAgentId] = useState('');
  const [goal, setGoal] = useState('');
  const [sampleKind, setSampleKind] = useState<SampleKind>('dataset');
  const [datasetId, setDatasetId] = useState('');
  const [positive, setPositive] = useState('');
  const [negative, setNegative] = useState('');
  const [body, setBody] = useState('');
  const [error, setError] = useState('');
  const agents = useQuery({
    queryKey: ['evaluation-rubric-agents', scope.workspaceId],
    queryFn: () => listAgents(scope.workspaceId),
    enabled: open && scope.ready && source === 'generate',
  });
  const datasets = useQuery({
    queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId],
    queryFn: () => listEvaluationDatasets(scope.orgUuid, scope.workspaceId),
    enabled: open && scope.ready && source === 'generate',
  });
  const create = useMutation({
    mutationFn: () =>
      submitRubric(scope.orgUuid, scope.workspaceId, source, draft(msg('evaluations.rubricDefaultName', 'Rubric'))),
    onSuccess: async () => {
      reset();
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-rubrics', scope.orgUuid, scope.workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function draft(fallbackName: string) {
    return {
      name: name.trim() || fallbackName,
      agentId,
      goal,
      datasetId,
      positive,
      negative,
      body,
    };
  }

  function reset() {
    setName('');
    setSource('generate');
    setAgentId('');
    setGoal('');
    setSampleKind('dataset');
    setDatasetId('');
    setPositive('');
    setNegative('');
    setBody('');
    setError('');
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (source === 'write' && !body.trim()) {
      setError(msg('evaluations.rubricContentRequired', 'Write the rubric.'));
      return;
    }
    if (source === 'generate' && !goal.trim() && !datasetId && !positive.trim() && !negative.trim()) {
      setError(msg('evaluations.generateNeedInput', 'Add a goal or a sample.'));
      return;
    }
    create.mutate();
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent
        className="max-h-[calc(100vh-4rem)] gap-5 overflow-y-auto p-6 sm:max-w-xl"
        closeButtonDisabled={create.isPending}
      >
        <form className="grid gap-5" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{msg('evaluations.createRubric', 'Create rubric')}</DialogTitle>
            <DialogDescription>
              {msg('evaluations.rubricIntro', 'Generate a rubric or write one yourself.')}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-5">
            <p className="text-sm font-medium">{msg('evaluations.basicInfo', 'Basics')}</p>
            <Field label={msg('evaluations.rubricTitleOptional', 'Title, optional')}>
              <Input
                id="evaluation-rubric-name"
                value={name}
                placeholder={msg('evaluations.rubricTitlePlaceholder', 'For example: support answer quality')}
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            <Field label={msg('evaluations.createMethod', 'How to create it')}>
              <Tabs value={source} onValueChange={(value) => setSource(value === 'write' ? 'write' : 'generate')}>
                <TabsList className="h-9">
                  <TabsTrigger className="px-3 data-active:border-foreground" value="generate">
                    {msg('evaluations.generateRubric', 'Generate')}
                  </TabsTrigger>
                  <TabsTrigger className="px-3 data-active:border-foreground" value="write">
                    {msg('evaluations.writeRubric', 'Write')}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="generate" className="grid gap-5 pt-5">
                  <GenerateBasis
                    agentId={agentId}
                    agents={(agents.data?.data ?? []).map((agent) => ({ id: agent.id, name: agent.name }))}
                    goal={goal}
                    sampleKind={sampleKind}
                    datasetId={datasetId}
                    datasets={(datasets.data?.data ?? []).map((dataset) => ({ id: dataset.id, name: dataset.name }))}
                    positive={positive}
                    negative={negative}
                    onAgentId={setAgentId}
                    onGoal={setGoal}
                    onSampleKind={setSampleKind}
                    onDatasetId={setDatasetId}
                    onPositive={setPositive}
                    onNegative={setNegative}
                  />
                </TabsContent>
                <TabsContent value="write" className="grid gap-5 pt-5">
                  <ManualRubric body={body} onBody={setBody} />
                </TabsContent>
              </Tabs>
            </Field>
          </div>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={create.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={create.isPending}>
              {source === 'generate'
                ? msg('evaluations.generateRubricAction', 'Generate rubric')
                : msg('evaluations.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function GenerateBasis({
  agentId,
  agents,
  goal,
  sampleKind,
  datasetId,
  datasets,
  positive,
  negative,
  onAgentId,
  onGoal,
  onSampleKind,
  onDatasetId,
  onPositive,
  onNegative,
}: {
  agentId: string;
  agents: Array<{ id: string; name: string }>;
  goal: string;
  sampleKind: SampleKind;
  datasetId: string;
  datasets: Array<{ id: string; name: string }>;
  positive: string;
  negative: string;
  onAgentId: (value: string) => void;
  onGoal: (value: string) => void;
  onSampleKind: (value: SampleKind) => void;
  onDatasetId: (value: string) => void;
  onPositive: (value: string) => void;
  onNegative: (value: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <p className="text-sm font-medium">{msg('evaluations.generateBasis', 'What to generate from')}</p>
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.generateBasisHint', 'Add a goal or a sample. The agent is optional.')}
        </p>
      </div>
      <Field label={msg('evaluations.agentOptionalSelect', 'Agent, optional')}>
        <Select
          value={agentId || null}
          items={agents.map((agent) => ({ value: agent.id, label: agent.name }))}
          onValueChange={(value) => onAgentId(value ?? '')}
        >
          <SelectTrigger className="w-full" aria-label={msg('evaluations.agentOptionalSelect', 'Agent, optional')}>
            <SelectValue placeholder={msg('evaluations.chooseAgentBackground', 'Choose an agent for extra context')} />
          </SelectTrigger>
          <SelectContent>
            {agents.map((agent) => (
              <SelectItem key={agent.id} value={agent.id}>
                {agent.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field label={msg('evaluations.goalOptionalLabel', 'Goal, optional')}>
        <Input
          id="evaluation-rubric-goal"
          value={goal}
          placeholder={msg(
            'evaluations.goalPlaceholder',
            'For example: whether the answer actually solves the problem.',
          )}
          onChange={(event) => onGoal(event.target.value)}
        />
      </Field>
      <Field label={msg('evaluations.sampleSource', 'Samples, optional')}>
        <Tabs value={sampleKind} onValueChange={(value) => onSampleKind(sampleKindFrom(value))}>
          <TabsList className="h-9">
            <TabsTrigger className="px-3 data-active:border-foreground" value="dataset">
              {msg('evaluations.sampleDataset', 'Dataset')}
            </TabsTrigger>
            <TabsTrigger className="px-3 data-active:border-foreground" value="positive">
              {msg('evaluations.samplePositive', 'Positive')}
            </TabsTrigger>
            <TabsTrigger className="px-3 data-active:border-foreground" value="negative">
              {msg('evaluations.sampleNegative', 'Negative')}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="dataset" className="pt-3">
            <Select
              value={datasetId || null}
              items={datasets.map((dataset) => ({ value: dataset.id, label: dataset.name }))}
              onValueChange={(value) => onDatasetId(value ?? '')}
            >
              <SelectTrigger className="w-full" aria-label={msg('evaluations.sampleDataset', 'Dataset')}>
                <SelectValue placeholder={msg('evaluations.chooseDataset', 'Choose a dataset')} />
              </SelectTrigger>
              <SelectContent>
                {datasets.map((dataset) => (
                  <SelectItem key={dataset.id} value={dataset.id}>
                    {dataset.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </TabsContent>
          <TabsContent value="positive" className="pt-3">
            <Textarea
              value={positive}
              rows={4}
              placeholder={msg(
                'evaluations.positivePlaceholder',
                'For example: the answer includes the order number and arrival time.',
              )}
              onChange={(event) => onPositive(event.target.value)}
            />
          </TabsContent>
          <TabsContent value="negative" className="pt-3">
            <Textarea
              value={negative}
              rows={4}
              placeholder={msg('evaluations.negativePlaceholder', 'For example: the answer invents a refund policy.')}
              onChange={(event) => onNegative(event.target.value)}
            />
          </TabsContent>
        </Tabs>
      </Field>
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.sampleCombineHint',
          'These can be combined. The dataset and any positive or negative examples are used together.',
        )}
      </p>
    </div>
  );
}

function ManualRubric({ body, onBody }: { body: string; onBody: (value: string) => void }) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <p className="text-sm font-medium">{msg('evaluations.scoreRules', 'Scoring rules')}</p>
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.scoreRulesHint', 'Describe the dimensions, the checks, and the scores.')}
        </p>
      </div>
      <Field label={msg('evaluations.rubricContent', 'Rubric')} required>
        <Textarea
          id="evaluation-rubric-body"
          className="min-h-52"
          value={body}
          rows={10}
          placeholder={rubricTemplate}
          onChange={(event) => onBody(event.target.value)}
        />
      </Field>
    </div>
  );
}

function Field({ label, required, children }: { label: string; required?: boolean; children: ReactNode }) {
  return (
    <div className="grid gap-2">
      <Label>
        {label}
        {required ? <span className="text-destructive">*</span> : null}
      </Label>
      {children}
    </div>
  );
}

function sampleKindFrom(value: unknown): SampleKind {
  if (value === 'positive' || value === 'negative') return value;
  return 'dataset';
}

function submitRubric(
  orgUuid: string,
  workspaceId: string,
  source: RubricSource,
  input: {
    name: string;
    agentId: string;
    goal: string;
    datasetId: string;
    positive: string;
    negative: string;
    body: string;
  },
) {
  if (source === 'generate') {
    return generateEvaluationRubric(orgUuid, workspaceId, {
      name: input.name,
      desc: '',
      pass_score: 60,
      goal: input.goal,
      agent_id: input.agentId || undefined,
      dataset_id: input.datasetId || undefined,
      positive: input.positive,
      negative: input.negative,
    });
  }
  return createEvaluationRubric(orgUuid, workspaceId, {
    name: input.name,
    desc: '',
    body: input.body,
    pass_score: 60,
  });
}
