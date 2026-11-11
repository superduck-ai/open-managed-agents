import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { areMemoryAttachesValid } from '../managed-agents/resources/memory-attach';
import { areSessionFileResourcesValid } from '../managed-agents/sessions/file-resource-form';
import type { MemoryAttachFormValue, SessionFileResourceFormValue } from '../managed-agents/types';
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
import { Label } from '../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Textarea } from '../../shared/ui/textarea';
import { createEvaluationTask, evaluationError, type EvaluationTaskInput } from './api';
import { DatasetCreateDialog } from './DatasetCreateDialog';
import { RubricCreateDialog } from './RubricCreateDialog';
import { taskMountResources, TaskMountFields } from './TaskMountFields';
import { listTaskAgents, listTaskDatasets, listTaskEntities, listTaskRubrics } from './taskOptions';
import { useEvaluationScope } from './useEvaluationScope';

export type EvaluationCreateKind = 'evaluate' | 'dataset' | 'rubric';

export function EvaluationCreateDialog({
  kind,
  open,
  onOpenChange,
}: {
  kind: EvaluationCreateKind | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  if (kind === 'dataset') return <DatasetCreateDialog open={open} onOpenChange={onOpenChange} />;
  if (kind === 'rubric') return <RubricCreateDialog open={open} onOpenChange={onOpenChange} />;
  return <TaskDialog open={open} onOpenChange={onOpenChange} />;
}

function TaskDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<EvaluationTaskInput>(emptyTask);
  const [vaultIds, setVaultIds] = useState<string[]>([]);
  const [files, setFiles] = useState<SessionFileResourceFormValue[]>([]);
  const [memories, setMemories] = useState<MemoryAttachFormValue[]>([]);
  const [error, setError] = useState('');
  const ready = open && scope.ready;
  const agents = useQuery({
    queryKey: ['evaluation-agents', scope.workspaceId],
    queryFn: () => listTaskAgents(scope.workspaceId),
    enabled: ready,
  });
  const environments = useQuery({
    queryKey: ['evaluation-environments', scope.workspaceId],
    queryFn: () => listTaskEntities('environments', scope.workspaceId),
    enabled: ready,
  });
  const datasets = useQuery({
    queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId],
    queryFn: () => listTaskDatasets(scope.orgUuid, scope.workspaceId),
    enabled: ready,
  });
  const rubrics = useQuery({
    queryKey: ['evaluation-rubrics', scope.orgUuid, scope.workspaceId],
    queryFn: () => listTaskRubrics(scope.orgUuid, scope.workspaceId),
    enabled: ready,
  });
  const create = useMutation({
    mutationFn: () =>
      createEvaluationTask(scope.orgUuid, scope.workspaceId, {
        ...draft,
        name: draft.name.trim() || fallbackTaskName(agentLabel(agents.data ?? [], draft.agent_id)),
        vault_ids: vaultIds,
        resources: taskMountResources(files, memories),
        focus: draft.focus,
      }),
    onSuccess: async () => {
      setDraft(emptyTask);
      setVaultIds([]);
      setFiles([]);
      setMemories([]);
      setError('');
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-tasks', scope.orgUuid, scope.workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!areSessionFileResourcesValid(files) || !areMemoryAttachesValid(memories)) {
      setError(msg('evaluations.resourcesInvalid', 'Finish the resource selection before starting.'));
      return;
    }
    create.mutate();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex max-h-[calc(100vh-4rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-xl"
        closeButtonDisabled={create.isPending}
      >
        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={onSubmit}>
          <DialogHeader className="px-6 pt-6 pr-12">
            <DialogTitle>{msg('evaluations.createEvaluate', 'New evaluation')}</DialogTitle>
            <DialogDescription>
              {msg(
                'evaluations.createEvaluateHint',
                'Choose an agent and what to score. This run does not change the agent.',
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 flex-1 content-start gap-8 overflow-y-auto px-6 py-6">
            <section className="grid gap-5">
              <h3 className="text-sm font-medium">{msg('evaluations.agentConfig', 'Agent')}</h3>
              <OptionSelect
                label={msg('evaluations.agentRequired', 'Agent')}
                placeholder={msg('evaluations.chooseAgent', 'Choose an agent')}
                value={draft.agent_id}
                onChange={(agentId) => setDraft({ ...draft, agent_id: agentId })}
                options={(agents.data ?? []).map((agent) => ({ id: agent.id, label: agent.name }))}
                empty={msg('evaluations.emptyAgents', 'No agents yet')}
              />
              <OptionSelect
                label={msg('evaluations.environmentRequired', 'Runtime environment')}
                placeholder={msg('evaluations.chooseEnvironment', 'Choose a runtime environment')}
                value={draft.environment_id}
                onChange={(environmentId) => setDraft({ ...draft, environment_id: environmentId })}
                options={(environments.data ?? []).map((item) => ({
                  id: item.id,
                  label: entityName(item),
                }))}
                empty={msg('evaluations.emptyEnvironments', 'No environments yet')}
              />
              <TaskMountFields
                workspaceId={scope.workspaceId}
                enabled={ready}
                vaultIds={vaultIds}
                files={files}
                memories={memories}
                onVaultIds={setVaultIds}
                onFiles={setFiles}
                onMemories={setMemories}
              />
            </section>
            <section className="grid gap-5">
              <h3 className="text-sm font-medium">{msg('evaluations.evalConfig', 'Evaluation')}</h3>
              <OptionSelect
                label={msg('evaluations.datasetRequired', 'Dataset')}
                placeholder={msg('evaluations.chooseDataset', 'Choose a dataset')}
                value={draft.dataset_id}
                onChange={(datasetId) => setDraft({ ...draft, dataset_id: datasetId })}
                options={(datasets.data ?? []).map((item) => ({ id: item.id, label: item.name }))}
                empty={msg('evaluations.emptyDatasets', 'No datasets yet')}
              />
              <OptionSelect
                label={msg('evaluations.rubricRequired', 'Rubric')}
                placeholder={msg('evaluations.chooseRubric', 'Choose a rubric')}
                value={draft.rubric_id}
                onChange={(rubricId) => setDraft({ ...draft, rubric_id: rubricId })}
                options={(rubrics.data ?? []).map((item) => ({ id: item.id, label: item.name }))}
                empty={msg('evaluations.emptyRubrics', 'No rubrics yet')}
              />
              <CountedNote
                id="evaluation-focus"
                label={msg('evaluations.focus', 'What to pay attention to, optional')}
                placeholder={msg(
                  'evaluations.focusPlaceholder',
                  'For example: check whether package prices are accurate.',
                )}
                value={draft.focus ?? ''}
                max={300}
                onChange={(focus) => setDraft({ ...draft, focus })}
              />
            </section>
          </div>
          {error ? <p className="px-6 pb-2 text-sm text-destructive">{error}</p> : null}
          <DialogActions
            className="border-t border-border px-6 py-4"
            pending={create.isPending}
            label={msg('evaluations.startEvaluate', 'Start evaluation')}
          />
        </form>
      </DialogContent>
    </Dialog>
  );
}

const emptyTask: EvaluationTaskInput = {
  name: '',
  mode: 'evaluate',
  agent_id: '',
  environment_id: '',
  dataset_id: '',
  rubric_id: '',
};

function fallbackTaskName(agentName: string) {
  if (!agentName) return '评测任务';
  return `${agentName} 评测`;
}

function agentLabel(agents: Array<{ id: string; name: string }>, agentId: string) {
  return agents.find((agent) => agent.id === agentId)?.name ?? '';
}

function entityName(item: { id: string; name?: unknown }) {
  return typeof item.name === 'string' && item.name ? item.name : item.id;
}

function CountedNote({
  id,
  label,
  placeholder,
  value,
  max,
  onChange,
}: {
  id: string;
  label: string;
  placeholder: string;
  value: string;
  max: number;
  onChange: (value: string) => void;
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Textarea
        id={id}
        value={value}
        maxLength={max}
        rows={3}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      <p className="text-right text-xs text-muted-foreground">
        {value.length}/{max}
      </p>
    </div>
  );
}

function OptionSelect({
  label,
  placeholder,
  value,
  options,
  empty,
  onChange,
}: {
  label: string;
  placeholder: string;
  value: string;
  options: Array<{ id: string; label: string }>;
  empty: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      <Select
        value={value || null}
        items={options.map((option) => ({ value: option.id, label: option.label }))}
        onValueChange={(next) => onChange(next ?? '')}
        emptyLabel={empty}
      >
        <SelectTrigger aria-label={label} className="w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.id} value={option.id}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function DialogActions({ pending, label, className }: { pending: boolean; label: string; className?: string }) {
  const { msg } = useI18n();
  return (
    <DialogFooter className={className}>
      <DialogClose render={<Button type="button" variant="outline" disabled={pending} />}>
        {msg('common.cancel', 'Cancel')}
      </DialogClose>
      <Button type="submit" disabled={pending}>
        {label}
      </Button>
    </DialogFooter>
  );
}
