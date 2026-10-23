import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationDatasetPreset } from './api';
import {
  useCreateEvaluationDataset,
  useEvaluationDatasetPresetCases,
  useEvaluationDatasetPresetList,
} from './useEvaluationDatasets';

export function DatasetPresets({ onReused }: { onReused: () => void }) {
  const { msg } = useI18n();
  const { query } = useEvaluationDatasetPresetList();
  const [preset, setPreset] = useState<EvaluationDatasetPreset | null>(null);
  const presets = query.data?.data ?? [];

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
            <TableHead>{msg('evaluations.presetVersion', 'Version')}</TableHead>
            <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
            <TableHead>{msg('evaluations.descriptionField', 'Description')}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          <DatasetPresetRows
            presets={presets}
            isLoading={query.isLoading}
            errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
            onRetry={() => void query.refetch()}
            onReuse={setPreset}
          />
        </TableBody>
      </Table>
      {preset ? (
        <ReuseDatasetPresetDialog key={preset.id} preset={preset} onClose={() => setPreset(null)} onReused={onReused} />
      ) : null}
    </>
  );
}

function DatasetPresetRows({
  presets,
  isLoading,
  errorMessage,
  onRetry,
  onReuse,
}: {
  presets: EvaluationDatasetPreset[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
  onReuse: (preset: EvaluationDatasetPreset) => void;
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
      <TableCell>{preset.versions.map((version) => `v${version.version}`).join(', ')}</TableCell>
      <TableCell>{presetStatusLabel(preset.status, msg)}</TableCell>
      <TableCell className="text-muted-foreground">{preset.desc}</TableCell>
      <TableCell>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={preset.status === 'archived'}
          aria-label={`${msg('evaluations.reusePreset', 'Reuse')} ${preset.name}`}
          onClick={() => onReuse(preset)}
        >
          {msg('evaluations.reusePreset', 'Reuse')}
        </Button>
      </TableCell>
    </TableRow>
  ));
}

function ReuseDatasetPresetDialog({
  preset,
  onClose,
  onReused,
}: {
  preset: EvaluationDatasetPreset;
  onClose: () => void;
  onReused: () => void;
}) {
  const { msg } = useI18n();
  const create = useCreateEvaluationDataset();
  const [versionId, setVersionId] = useState(preset.versions[preset.versions.length - 1]?.id ?? '');
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const casesQuery = useEvaluationDatasetPresetCases(preset.id, versionId);
  const versionItems = preset.versions.map((version) => ({
    value: version.id,
    label: `v${version.version}`,
  }));

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, desc, source_version_id: versionId },
      {
        onSuccess: () => {
          onClose();
          onReused();
        },
      },
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{msg('evaluations.reusePreset', 'Reuse')}</DialogTitle>
        </DialogHeader>
        <form className="grid gap-3" onSubmit={submit}>
          <p className="text-sm text-muted-foreground">{preset.name}</p>
          {preset.versions.length > 1 ? (
            <Select
              value={versionId}
              items={versionItems}
              onValueChange={(value) => {
                if (value) {
                  setVersionId(value);
                }
              }}
            >
              <SelectTrigger aria-label={msg('evaluations.presetVersion', 'Version')} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {versionItems.map((item) => (
                  <SelectItem key={item.value} value={item.value} label={item.label}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
          <Label htmlFor="reuse-dataset-name">{msg('evaluations.name', 'Name')}</Label>
          <Input id="reuse-dataset-name" value={name} onChange={(event) => setName(event.target.value)} required />
          <Label htmlFor="reuse-dataset-description">{msg('evaluations.descriptionField', 'Description')}</Label>
          <Input id="reuse-dataset-description" value={desc} onChange={(event) => setDesc(event.target.value)} />
          <ul className="grid gap-1 text-sm text-muted-foreground">
            {(casesQuery.data?.data ?? []).map((item) => (
              <li key={item.id}>{item.turns.join(' / ')}</li>
            ))}
          </ul>
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <DialogFooter>
            <Button type="submit" disabled={create.isPending || versionId.length === 0}>
              {msg('evaluations.reusePreset', 'Reuse')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function presetStatusLabel(status: EvaluationDatasetPreset['status'], msg: ReturnType<typeof useI18n>['msg']) {
  if (status === 'archived') {
    return msg('evaluations.archived', 'Archived');
  }
  return msg('evaluations.available', 'Available');
}
