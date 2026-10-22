import { useState, type ChangeEvent, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationErrorMessage } from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { evaluationExperimentHref } from './useEvaluationDatasets';
import { useCreateEvaluationExperiment, useEvaluationExperimentList } from './useEvaluationExperiments';

type ExperimentDraft = {
  name: string;
  desc: string;
  datasetVersionId: string;
  agentId: string;
  agentVersion: string;
  environmentId: string;
  timeoutSeconds: string;
  evaluatorIds: string;
};

const emptyDraft: ExperimentDraft = {
  name: '',
  desc: '',
  datasetVersionId: '',
  agentId: '',
  agentVersion: '',
  environmentId: '',
  timeoutSeconds: '',
  evaluatorIds: '',
};

export function ExperimentNewPage() {
  const { msg } = useI18n();
  const { workspace } = useEvaluationExperimentList();
  const create = useCreateEvaluationExperiment();
  const [draft, setDraft] = useState(emptyDraft);
  const [createdId, setCreatedId] = useState('');

  function setField(key: keyof ExperimentDraft) {
    return (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      const value = event.target.value;
      setDraft((current) => ({ ...current, [key]: value }));
    };
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const agentVersion = draft.agentVersion.trim();
    create.mutate(
      {
        name: draft.name,
        desc: draft.desc,
        dataset_version_id: draft.datasetVersionId,
        agent_id: draft.agentId,
        ...(agentVersion ? { agent_version: Number(agentVersion) } : {}),
        environment_id: draft.environmentId,
        timeout_seconds: Number(draft.timeoutSeconds),
        evaluator_ids: splitIds(draft.evaluatorIds),
      },
      { onSuccess: (experiment) => setCreatedId(experiment.id) },
    );
  }

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <header className="mb-5">
        <h1 className="text-2xl font-semibold tracking-tight">
          {msg('evaluations.createExperiment', 'New experiment')}
        </h1>
      </header>
      <form className="grid max-w-xl gap-4" onSubmit={onSubmit}>
        <Field label={msg('evaluations.name', 'Name')} name="name" value={draft.name} onChange={setField('name')} />
        <Field
          label={msg('evaluations.descriptionField', 'Description')}
          name="desc"
          value={draft.desc}
          onChange={setField('desc')}
        />
        <Field
          label={msg('evaluations.datasetVersion', 'Dataset version')}
          name="dataset_version_id"
          value={draft.datasetVersionId}
          onChange={setField('datasetVersionId')}
        />
        <Field
          label={msg('evaluations.agentId', 'Agent')}
          name="agent_id"
          value={draft.agentId}
          onChange={setField('agentId')}
        />
        <Field
          label={msg('evaluations.agentVersion', 'Agent version')}
          name="agent_version"
          value={draft.agentVersion}
          onChange={setField('agentVersion')}
        />
        <Field
          label={msg('evaluations.environmentId', 'Environment')}
          name="environment_id"
          value={draft.environmentId}
          onChange={setField('environmentId')}
        />
        <Field
          label={msg('evaluations.timeoutSeconds', 'Timeout (seconds)')}
          name="timeout_seconds"
          value={draft.timeoutSeconds}
          onChange={setField('timeoutSeconds')}
        />
        <div className="grid gap-2">
          <Label htmlFor="evaluator_ids">{msg('evaluations.evaluatorIds', 'Evaluators')}</Label>
          <Textarea id="evaluator_ids" value={draft.evaluatorIds} onChange={setField('evaluatorIds')} />
        </div>
        {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
        {createdId ? (
          <a
            className="text-sm font-medium hover:underline"
            href={evaluationExperimentHref(workspace.workspaceId, createdId)}
          >
            {createdId}
          </a>
        ) : null}
        <Button type="submit" className="h-9 w-fit" disabled={create.isPending}>
          {msg('evaluations.createExperiment', 'New experiment')}
        </Button>
      </form>
    </section>
  );
}

function Field({
  label,
  name,
  value,
  onChange,
}: {
  label: string;
  name: string;
  value: string;
  onChange: (event: ChangeEvent<HTMLInputElement>) => void;
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={name}>{label}</Label>
      <Input id={name} value={value} onChange={onChange} />
    </div>
  );
}

function splitIds(value: string) {
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}
