import type { ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { CopyButton } from '../../shared/ui/copy-button';
import type { EvaluationTask } from './api';
import { formatEvaluationTime } from './evaluationChrome';
import { TaskSection } from './TaskSection';

type InfoRow = { label: string; value: ReactNode };

function formatTokens(value: number | undefined) {
  return value == null ? '—' : value.toLocaleString();
}

function IdValue({ value, copyLabel }: { value: string; copyLabel: string }) {
  if (!value) return <>—</>;
  return (
    <span className="inline-flex min-w-0 items-center gap-1 font-mono text-xs">
      <span className="truncate">{value}</span>
      <CopyButton value={value} label={copyLabel} />
    </span>
  );
}

function NamedValue({ name, id, copyLabel }: { name?: string; id: string; copyLabel: string }) {
  if (!name) return <IdValue value={id} copyLabel={copyLabel} />;
  return (
    <span className="grid gap-0.5">
      <span>{name}</span>
      <span className="font-mono text-xs text-muted-foreground">{id}</span>
    </span>
  );
}

function InfoGrid({ title, rows }: { title: string; rows: InfoRow[] }) {
  return (
    <section className="overflow-hidden rounded-xl border border-border">
      <h3 className="border-b border-border bg-muted/40 px-4 py-2.5 text-sm font-medium">{title}</h3>
      <dl className="-mb-px grid sm:grid-cols-2 xl:grid-cols-3">
        {rows.map((row) => (
          <div key={row.label} className="grid content-start gap-1 border-b border-border px-4 py-3">
            <dt className="text-xs text-muted-foreground">{row.label}</dt>
            <dd className="min-w-0 text-sm break-words">{row.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

export function TaskInfoPanel({ task }: { task: EvaluationTask }) {
  const { msg } = useI18n();
  const copyLabel = msg('evaluations.copyId', 'Copy ID');
  const countLabel = (count: number) =>
    count > 0 ? msg('evaluations.itemCount', '{count} items', { count }) : msg('evaluations.notMounted', 'None');
  const basic: InfoRow[] = [
    { label: msg('evaluations.taskId', 'Task ID'), value: <IdValue value={task.id} copyLabel={copyLabel} /> },
    { label: msg('evaluations.taskTypeLabel', 'Task type'), value: msg('evaluations.taskType', 'Evaluation task') },
    {
      label: msg('evaluations.agent', 'Agent'),
      value: <NamedValue name={task.agent_name} id={task.agent_id} copyLabel={copyLabel} />,
    },
    { label: msg('evaluations.agentVersion', 'Agent version'), value: `v${task.agent_version}` },
    { label: msg('evaluations.createdAt', 'Created'), value: formatEvaluationTime(task.created_at) },
  ];
  const config: InfoRow[] = [
    {
      label: msg('evaluations.datasetDetail', 'Dataset'),
      value: <NamedValue name={task.dataset_name} id={task.dataset_id} copyLabel={copyLabel} />,
    },
    {
      label: msg('evaluations.rubrics', 'Rubric'),
      value: <NamedValue name={task.rubric_name} id={task.rubric_id} copyLabel={copyLabel} />,
    },
    { label: msg('evaluations.passScore', 'Pass score'), value: task.pass_score },
  ];
  const resources: InfoRow[] = [
    {
      label: msg('evaluations.environment', 'Environment'),
      value: <IdValue value={task.environment_id} copyLabel={copyLabel} />,
    },
    { label: msg('evaluations.inputTokens', 'Input tokens'), value: formatTokens(task.usage?.input_tokens) },
    { label: msg('evaluations.outputTokens', 'Output tokens'), value: formatTokens(task.usage?.output_tokens) },
    {
      label: msg('evaluations.cacheReadTokens', 'Cache read tokens'),
      value: formatTokens(task.usage?.cache_read_input_tokens),
    },
    {
      label: msg('evaluations.judgeInputTokens', 'Judge input tokens'),
      value: formatTokens(task.usage?.judge_input_tokens),
    },
    {
      label: msg('evaluations.judgeOutputTokens', 'Judge output tokens'),
      value: formatTokens(task.usage?.judge_output_tokens),
    },
    { label: msg('evaluations.mountedResources', 'Mounted resources'), value: countLabel(task.resource_count) },
    { label: msg('evaluations.vaults', 'Vaults'), value: countLabel(task.vault_count) },
  ];
  return (
    <TaskSection
      title={msg('evaluations.taskInfo', 'Task info')}
      hint={msg('evaluations.taskInfoHint', 'Configuration, evaluation basis, and usage for this task.')}
    >
      {task.error_message ? (
        <p className="rounded-lg bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {msg('evaluations.taskError', 'Failure reason')}: {task.error_message}
        </p>
      ) : null}
      <InfoGrid title={msg('evaluations.basicInfo', 'Basic info')} rows={basic} />
      <InfoGrid title={msg('evaluations.evalConfig', 'Evaluation config')} rows={config} />
      <InfoGrid title={msg('evaluations.executionResources', 'Execution and resources')} rows={resources} />
    </TaskSection>
  );
}
