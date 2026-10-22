import { Plus } from 'lucide-react';
import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationDataset } from './api';
import { DatasetPresets } from './DatasetPresets';
import { EvaluationOriginTabs, type EvaluationOrigin } from './EvaluationOriginTabs';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { evaluationDatasetHref, useCreateEvaluationDataset, useEvaluationDatasetList } from './useEvaluationDatasets';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationDatasetList();
  const [createOpen, setCreateOpen] = useState(false);
  const [section, setSection] = useState<EvaluationOrigin>('custom');
  const datasets = query.data?.data ?? [];

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="datasets" />
      <header className="mb-5 flex items-start justify-between gap-6">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            {msg('evaluations.title', 'Datasets')}
          </h1>
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
            {msg('evaluations.description', 'Reusable cases for the {workspaceName} workspace.', {
              workspaceName: workspace.workspaceName,
            })}
          </p>
        </div>
        {section === 'custom' ? (
          <Button type="button" className="h-9 shrink-0" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden />
            {msg('evaluations.create', 'Create dataset')}
          </Button>
        ) : null}
      </header>
      <EvaluationOriginTabs section={section} onSection={setSection} />
      {section === 'presets' ? (
        <DatasetPresets onReused={() => setSection('custom')} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
              <TableHead>{msg('evaluations.descriptionField', 'Description')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <DatasetRows
              datasets={datasets}
              workspaceId={workspace.workspaceId}
              isLoading={!workspace.workspaceReady || query.isLoading}
              errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
              onRetry={() => void query.refetch()}
            />
          </TableBody>
        </Table>
      )}
      <CreateDatasetDialog open={createOpen} onOpenChange={setCreateOpen} />
    </section>
  );
}

function DatasetRows({
  datasets,
  workspaceId,
  isLoading,
  errorMessage,
  onRetry,
}: {
  datasets: EvaluationDataset[];
  workspaceId: string;
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={2} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={2}
        title={msg('evaluations.loadFailed', 'Could not load datasets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (datasets.length === 0) {
    return <TableEmptyRow colSpan={2}>{msg('evaluations.empty', 'No datasets yet.')}</TableEmptyRow>;
  }
  return datasets.map((dataset) => (
    <TableRow key={dataset.id}>
      <TableCell>
        <a
          className="font-medium text-foreground underline-offset-4 hover:underline"
          href={evaluationDatasetHref(workspaceId, dataset.id)}
        >
          {dataset.name}
        </a>
      </TableCell>
      <TableCell className="text-muted-foreground">{dataset.desc}</TableCell>
    </TableRow>
  ));
}

function CreateDatasetDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const create = useCreateEvaluationDataset();
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, desc },
      {
        onSuccess: () => {
          setName('');
          setDesc('');
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{msg('evaluations.create', 'Create dataset')}</DialogTitle>
        </DialogHeader>
        <form className="grid gap-3" onSubmit={submit}>
          <Label htmlFor="evaluation-dataset-name">{msg('evaluations.name', 'Name')}</Label>
          <Input id="evaluation-dataset-name" value={name} onChange={(event) => setName(event.target.value)} required />
          <Label htmlFor="evaluation-dataset-description">{msg('evaluations.descriptionField', 'Description')}</Label>
          <Input id="evaluation-dataset-description" value={desc} onChange={(event) => setDesc(event.target.value)} />
          {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
          <DialogFooter>
            <Button type="submit" disabled={create.isPending}>
              {msg('evaluations.create', 'Create dataset')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
