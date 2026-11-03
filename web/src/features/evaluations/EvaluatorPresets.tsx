import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationEvaluatorPreset } from './api';
import { CopyIdButton } from './CopyIdButton';
import { evaluationEvaluatorPresetHref, useEvaluationWorkspace } from './useEvaluationDatasets';
import { useCreateEvaluationEvaluatorFromPreset, useEvaluationEvaluatorPresetList } from './useEvaluationEvaluators';

export function EvaluatorPresets({ search = '', onCreated }: { search?: string; onCreated: () => void }) {
  const { msg } = useI18n();
  const { query } = useEvaluationEvaluatorPresetList();
  const [preset, setPreset] = useState<EvaluationEvaluatorPreset | null>(null);
  const presets = (query.data?.data ?? []).filter((item) =>
    matchesPresetQuery(search, [item.version.id, item.name, item.desc]),
  );

  return (
    <>
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
            <TableHead className="w-36 text-right text-xs font-normal text-muted-foreground">
              {msg('evaluations.actions', 'Action')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <EvaluatorPresetRows
            presets={presets}
            isLoading={query.isLoading}
            errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
            onRetry={() => void query.refetch()}
            onCreate={setPreset}
          />
        </TableBody>
      </Table>
      {preset ? (
        <CreateFromEvaluatorPresetDialog
          key={preset.id}
          preset={preset}
          onClose={() => setPreset(null)}
          onCreated={onCreated}
        />
      ) : null}
    </>
  );
}

function EvaluatorPresetRows({
  presets,
  isLoading,
  errorMessage,
  onRetry,
  onCreate,
}: {
  presets: EvaluationEvaluatorPreset[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
  onCreate: (preset: EvaluationEvaluatorPreset) => void;
}) {
  const { msg } = useI18n();
  const workspace = useEvaluationWorkspace();
  if (isLoading) {
    return <TableLoadingRow colSpan={7} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={7}
        title={msg('evaluations.presetsLoadFailed', 'Could not load presets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (presets.length === 0) {
    return <TableEmptyRow colSpan={7}>{msg('evaluations.presetsEmpty', 'No presets.')}</TableEmptyRow>;
  }
  return presets.map((preset) => (
    <TableRow key={preset.id}>
      <TableCell>
        <CopyIdButton id={preset.version.id} />
      </TableCell>
      <TableCell className="font-medium">
        <a
          className="block truncate hover:underline"
          href={evaluationEvaluatorPresetHref(workspace.workspaceId, preset.id)}
        >
          {preset.name}
        </a>
      </TableCell>
      <TableCell className="text-muted-foreground">
        <span className="block truncate">{preset.desc}</span>
      </TableCell>
      <TableCell>{presetTypeLabel(preset.version.evaluator_type, msg)}</TableCell>
      <TableCell>
        <PresetVariables names={preset.version.variables} />
      </TableCell>
      <TableCell className="text-muted-foreground">{presetDate(preset.created_at)}</TableCell>
      <TableCell className="text-right">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-8 px-2 font-normal text-muted-foreground"
          disabled={preset.status === 'archived'}
          aria-label={`${msg('evaluations.createFromPreset', 'Create from preset')} ${preset.name}`}
          onClick={() => onCreate(preset)}
        >
          {msg('evaluations.createFromPreset', 'Create from preset')}
        </Button>
      </TableCell>
    </TableRow>
  ));
}

function PresetVariables({ names }: { names: string[] }) {
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

function presetTypeLabel(type: string, msg: ReturnType<typeof useI18n>['msg']) {
  if (type === 'llm') {
    return msg('evaluations.evaluatorLLM', 'LLM');
  }
  return msg('evaluations.evaluatorAgent', 'Agent');
}

function presetDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}

function matchesPresetQuery(query: string, parts: string[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.some((part) => part.toLowerCase().includes(needle));
}

function CreateFromEvaluatorPresetDialog({
  preset,
  onClose,
  onCreated,
}: {
  preset: EvaluationEvaluatorPreset;
  onClose: () => void;
  onCreated: () => void;
}) {
  const { msg } = useI18n();
  const create = useCreateEvaluationEvaluatorFromPreset();
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, desc, source_version_id: preset.version.id },
      {
        onSuccess: () => {
          onClose();
          onCreated();
        },
      },
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{msg('evaluations.createFromPreset', 'Create from preset')}</DialogTitle>
        </DialogHeader>
        <form className="grid gap-3" onSubmit={submit}>
          <p className="text-sm text-muted-foreground">
            {preset.name} · {preset.version.evaluator_type}
          </p>
          <Label htmlFor="preset-evaluator-name">{msg('evaluations.name', 'Name')}</Label>
          <Input id="preset-evaluator-name" value={name} onChange={(event) => setName(event.target.value)} required />
          <Label htmlFor="preset-evaluator-description">{msg('evaluations.descriptionField', 'Description')}</Label>
          <Input id="preset-evaluator-description" value={desc} onChange={(event) => setDesc(event.target.value)} />
          <p className="text-sm text-muted-foreground">
            {msg('evaluations.presetAgentHint', 'The preset rubric is copied.')}
          </p>
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <DialogFooter>
            <Button type="submit" disabled={create.isPending}>
              {msg('evaluations.createFromPreset', 'Create from preset')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
