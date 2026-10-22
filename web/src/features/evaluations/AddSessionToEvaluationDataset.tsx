import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { evaluationErrorMessage } from './api';
import { parseSessionCaseCheckpoints } from './sessionCase';
import { IncludeOutputFilesField, SessionCaseFields } from './SessionCaseFields';
import { useCreateEvaluationCaseFromSession, useEvaluationDatasetList } from './useEvaluationDatasets';

export function AddSessionToEvaluationDataset({ sessionId }: { sessionId: string }) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button type="button" variant="outline" onClick={() => setOpen(true)}>
        {msg('evaluations.addSessionToDataset', 'Add to evaluation dataset')}
      </Button>
      {open ? <AddSessionToDatasetDialog key={sessionId} sessionId={sessionId} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function AddSessionToDatasetDialog({ sessionId, onClose }: { sessionId: string; onClose: () => void }) {
  const { msg } = useI18n();
  const { query } = useEvaluationDatasetList();
  const datasets = (query.data?.data ?? []).filter((dataset) => dataset.status === 'available');
  const [datasetId, setDatasetId] = useState(datasets[0]?.id ?? '');
  const selectedDatasetId = datasets.some((dataset) => dataset.id === datasetId) ? datasetId : (datasets[0]?.id ?? '');
  const create = useCreateEvaluationCaseFromSession(selectedDatasetId);
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [checkpoints, setCheckpoints] = useState('');
  const [includeOutputFiles, setIncludeOutputFiles] = useState(false);
  const [checkpointError, setCheckpointError] = useState(false);

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = parseSessionCaseCheckpoints(checkpoints);
    if (parsed.error || selectedDatasetId === '') {
      setCheckpointError(Boolean(parsed.error));
      return;
    }
    setCheckpointError(false);
    create.mutate(
      {
        session_id: sessionId,
        name,
        desc,
        checkpoints: parsed.checkpoints,
        include_output_files: includeOutputFiles,
      },
      {
        onSuccess: () => {
          onClose();
        },
      },
    );
  }

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{msg('evaluations.addSessionToDataset', 'Add to evaluation dataset')}</DialogTitle>
        </DialogHeader>
        <form className="grid gap-3" onSubmit={submit}>
          <p className="text-sm text-muted-foreground">
            {msg(
              'evaluations.addSessionToDatasetHint',
              'User messages become turns. Agent replies are not saved as checkpoints.',
            )}
          </p>
          <DatasetSelect
            datasets={datasets}
            value={selectedDatasetId}
            onChange={setDatasetId}
            loading={query.isLoading}
          />
          <SessionCaseFields
            idPrefix="session-case"
            name={name}
            desc={desc}
            checkpoints={checkpoints}
            checkpointError={checkpointError}
            onName={setName}
            onDesc={setDesc}
            onCheckpoints={setCheckpoints}
          />
          <IncludeOutputFilesField
            id="session-case-outputs"
            checked={includeOutputFiles}
            onChange={setIncludeOutputFiles}
          />
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <DialogFooter>
            <Button type="submit" disabled={create.isPending || selectedDatasetId === ''}>
              {msg('evaluations.addSessionToDataset', 'Add to evaluation dataset')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DatasetSelect({
  datasets,
  value,
  onChange,
  loading,
}: {
  datasets: Array<{ id: string; name: string }>;
  value: string;
  onChange: (value: string) => void;
  loading: boolean;
}) {
  const { msg } = useI18n();
  if (!loading && datasets.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">{msg('evaluations.noCustomDatasets', 'No custom datasets yet.')}</p>
    );
  }
  const items = datasets.map((dataset) => ({ value: dataset.id, label: dataset.name }));
  return (
    <Select
      value={value}
      items={items}
      onValueChange={(next) => {
        if (next) {
          onChange(next);
        }
      }}
    >
      <SelectTrigger aria-label={msg('evaluations.dataset', 'Dataset')} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value} label={item.label}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
