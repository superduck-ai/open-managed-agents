import { useQueryClient } from '@tanstack/react-query';
import { CircleHelp, XIcon } from 'lucide-react';
import { useState, type FormEvent } from 'react';

import { useAuth } from '../../shared/auth/context';
import { useI18n, useLocale } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Tabs, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationErrorMessage, importEvaluationCases } from './api';
import { DatasetFormatHelp } from './DatasetFormatHelp';
import { DatasetReusePicker, useDatasetReuse } from './DatasetReusePicker';
import { DatasetUploadDropzone } from './DatasetUploadDropzone';
import { downloadDatasetTemplate } from './datasetTemplate';
import { datasetCreateInput } from './reuseSources';
import { useCreateEvaluationDataset, useEvaluationWorkspace } from './useEvaluationDatasets';

const maxImportBytes = 100 * 1024 * 1024;
const maxNameLength = 50;

type DatasetSource = 'upload' | 'reuse';

export function CreateDatasetDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const { locale } = useLocale();
  const create = useCreateEvaluationDataset();
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [source, setSource] = useState<DatasetSource>('upload');
  const [file, setFile] = useState<File | null>(null);
  const [helpOpen, setHelpOpen] = useState(false);
  const [tooLarge, setTooLarge] = useState(false);
  const [importError, setImportError] = useState('');
  const reuse = useDatasetReuse(open && source === 'reuse');

  function close(next: boolean) {
    if (!next) {
      setName('');
      setDesc('');
      setSource('upload');
      setFile(null);
      setTooLarge(false);
      setImportError('');
      reuse.reset();
    }
    onOpenChange(next);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (source === 'upload' && file && file.size > maxImportBytes) {
      setTooLarge(true);
      return;
    }
    setTooLarge(false);
    setImportError('');
    const createdId = await createDataset();
    if (!createdId) {
      return;
    }
    if (source === 'upload' && file) {
      const imported = await importFile(createdId, file);
      if (!imported) {
        return;
      }
    }
    close(false);
  }

  async function createDataset() {
    try {
      const created = await create.mutateAsync(
        datasetCreateInput(
          source,
          name,
          desc,
          reuse.option && reuse.version
            ? {
                kind: reuse.option.kind,
                id: reuse.option.id,
                versionId: reuse.version.id,
                caseIds: reuse.selected.map((item) => item.id),
              }
            : null,
        ),
      );
      return created.id;
    } catch {
      return '';
    }
  }

  async function importFile(datasetId: string, next: File) {
    try {
      await importEvaluationCases(
        workspace.orgUuid ?? '',
        workspace.workspaceId,
        datasetId,
        next,
        next.name.toLowerCase().endsWith('.zip') ? 'application/zip' : 'application/jsonl',
        csrfToken,
      );
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets'] });
      return true;
    } catch (error) {
      setImportError(
        `${msg('evaluations.importFailedAfterCreate', 'The dataset was created, but the file was not imported.')} ${evaluationErrorMessage(error)}`,
      );
      return false;
    }
  }

  const waitingForCases = source === 'reuse' && reuse.selected.length === 0;
  return (
    <>
      <Dialog open={open} onOpenChange={close}>
        <DialogContent className="gap-5 p-6 sm:max-w-4xl" showCloseButton={false}>
          <div className="flex items-center justify-between gap-3">
            <DialogTitle className="text-base font-semibold">
              {msg('evaluations.createTitle', 'New dataset')}
            </DialogTitle>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="text-muted-foreground"
                onClick={() => setHelpOpen(true)}
              >
                <CircleHelp className="size-4" aria-hidden />
                {msg('evaluations.createHelp', 'How to create a dataset')}
              </Button>
              <DialogClose render={<Button type="button" variant="ghost" size="icon-sm" />}>
                <XIcon />
                <span className="sr-only">Close</span>
              </DialogClose>
            </div>
          </div>
          <form className="grid gap-5" onSubmit={(event) => void submit(event)}>
            <div className="grid gap-2">
              <div className="flex items-center gap-0.5">
                <RequiredMark />
                <Label htmlFor="evaluation-dataset-name">{msg('evaluations.datasetName', 'Name')}</Label>
              </div>
              <Input
                id="evaluation-dataset-name"
                value={name}
                maxLength={maxNameLength}
                placeholder={msg('evaluations.datasetNamePlaceholder', 'e.g. Customer answer accuracy')}
                onChange={(event) => setName(event.target.value)}
                required
              />
              <p className="text-xs text-muted-foreground">
                {msg('evaluations.nameCount', '{count}/50', { count: name.length })}
              </p>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="evaluation-dataset-description">
                {msg('evaluations.descriptionField', 'Description')}
              </Label>
              <Textarea
                id="evaluation-dataset-description"
                value={desc}
                className="min-h-24"
                placeholder={msg('evaluations.descPlaceholder', 'What this dataset evaluates')}
                onChange={(event) => setDesc(event.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <p className="text-sm font-medium">{msg('evaluations.datasetSource', 'Dataset source')}</p>
              <Tabs
                value={source}
                onValueChange={(value) => {
                  if (value === 'upload' || value === 'reuse') {
                    setSource(value);
                  }
                }}
              >
                <TabsList>
                  <TabsTrigger type="button" value="upload" className="px-3">
                    {msg('evaluations.createUpload', 'Upload data')}
                  </TabsTrigger>
                  <TabsTrigger type="button" value="reuse" className="px-3">
                    {msg('evaluations.createReuse', 'Reuse existing')}
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            </div>
            {source === 'upload' ? (
              <DatasetUploadDropzone
                file={file}
                tooLarge={tooLarge}
                onFile={(next) => {
                  setFile(next);
                  setTooLarge(Boolean(next && next.size > maxImportBytes));
                }}
                onTemplate={() => downloadDatasetTemplate(locale)}
              />
            ) : (
              <DatasetReusePicker reuse={reuse} />
            )}
            {importError ? <p className="text-sm text-destructive">{importError}</p> : null}
            {create.error && !importError ? (
              <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p>
            ) : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => close(false)}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={create.isPending || (source === 'upload' && !file) || waitingForCases}>
                {msg('evaluations.create', 'Create dataset')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <DatasetFormatHelp open={helpOpen} onOpenChange={setHelpOpen} />
    </>
  );
}

function RequiredMark() {
  return (
    <span className="text-destructive" aria-hidden>
      *
    </span>
  );
}
