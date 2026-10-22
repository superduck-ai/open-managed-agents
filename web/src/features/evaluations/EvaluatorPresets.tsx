import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationEvaluatorPreset } from './api';
import { useCreateEvaluationEvaluatorFromPreset, useEvaluationEvaluatorPresetList } from './useEvaluationEvaluators';

export function EvaluatorPresets({ onCreated }: { onCreated: () => void }) {
  const { msg } = useI18n();
  const { query } = useEvaluationEvaluatorPresetList();
  const [preset, setPreset] = useState<EvaluationEvaluatorPreset | null>(null);
  const presets = query.data?.data ?? [];

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
            <TableHead>{msg('evaluations.evaluatorType', 'Type')}</TableHead>
            <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
            <TableHead>{msg('evaluations.descriptionField', 'Description')}</TableHead>
            <TableHead />
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
  if (isLoading) {
    return <TableLoadingRow colSpan={5} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={5}
        title={msg('evaluations.presetsLoadFailed', 'Could not load presets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (presets.length === 0) {
    return <TableEmptyRow colSpan={5}>{msg('evaluations.presetsEmpty', 'No presets.')}</TableEmptyRow>;
  }
  return presets.map((preset) => (
    <TableRow key={preset.id}>
      <TableCell className="font-medium">{preset.name}</TableCell>
      <TableCell>{preset.version.evaluator_type}</TableCell>
      <TableCell>
        {preset.status === 'archived'
          ? msg('evaluations.archived', 'Archived')
          : msg('evaluations.available', 'Available')}
      </TableCell>
      <TableCell className="text-muted-foreground">{preset.desc}</TableCell>
      <TableCell>
        <Button
          type="button"
          variant="outline"
          size="sm"
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
