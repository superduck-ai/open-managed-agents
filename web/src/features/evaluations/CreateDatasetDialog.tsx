import { useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { useAuth } from '../../shared/auth/context';
import { useI18n, useLocale } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import {
  evaluationErrorMessage,
  importEvaluationCases,
  type CreateEvaluationDatasetInput,
  type EvaluationDatasetPreset,
} from './api';
import { DatasetFormatHelp } from './DatasetFormatHelp';
import { downloadDatasetTemplate } from './datasetTemplate';
import {
  useCreateEvaluationDataset,
  useEvaluationDatasetPresetList,
  useEvaluationWorkspace,
} from './useEvaluationDatasets';

const maxImportBytes = 100 * 1024 * 1024;

type DatasetSource = 'blank' | 'upload' | 'reuse';

export function CreateDatasetDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const { locale } = useLocale();
  const create = useCreateEvaluationDataset();
  const { query } = useEvaluationDatasetPresetList(open && source === 'reuse');
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [source, setSource] = useState<DatasetSource>('blank');
  const [file, setFile] = useState<File | null>(null);
  const [presetId, setPresetId] = useState('');
  const [versionId, setVersionId] = useState('');
  const [helpOpen, setHelpOpen] = useState(false);
  const [tooLarge, setTooLarge] = useState(false);
  const [importError, setImportError] = useState('');
  const presets = (query.data?.data ?? []).filter(
    (preset) => preset.status === 'available' && Array.isArray(preset.versions),
  );
  const preset = presets.find((item) => item.id === presetId) ?? presets[0];
  const version = selectedVersion(preset, versionId);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (file && file.size > maxImportBytes) {
      setTooLarge(true);
      return;
    }
    setTooLarge(false);
    setImportError('');
    let createdId = '';
    try {
      const created = await create.mutateAsync(datasetCreateInput(source, name, desc, version));
      createdId = created.id;
    } catch {
      return;
    }
    if (source === 'upload' && file) {
      try {
        await importEvaluationCases(
          workspace.orgUuid ?? '',
          workspace.workspaceId,
          createdId,
          file,
          file.name.toLowerCase().endsWith('.zip') ? 'application/zip' : 'application/jsonl',
          csrfToken,
        );
        await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets'] });
      } catch (error) {
        setImportError(
          `${msg('evaluations.importFailedAfterCreate', 'The dataset was created, but the file was not imported.')} ${evaluationErrorMessage(error)}`,
        );
        return;
      }
    }
    setName('');
    setDesc('');
    setFile(null);
    onOpenChange(false);
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{msg('evaluations.create', 'Create dataset')}</DialogTitle>
          </DialogHeader>
          <form className="grid gap-3" onSubmit={(event) => void submit(event)}>
            <Button
              type="button"
              variant="link"
              className="h-auto justify-start px-0"
              onClick={() => setHelpOpen(true)}
            >
              {msg('evaluations.createHelp', 'How to create a dataset')}
            </Button>
            <p className="text-sm text-muted-foreground">
              {msg('evaluations.createSourceHint', 'Start empty, upload a JSONL or ZIP, or copy a preset.')}
            </p>
            <DatasetSourcePicker source={source} onSource={setSource} />
            <Label htmlFor="evaluation-dataset-name">{msg('evaluations.name', 'Name')}</Label>
            <Input
              id="evaluation-dataset-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              required
            />
            <Label htmlFor="evaluation-dataset-description">{msg('evaluations.descriptionField', 'Description')}</Label>
            <Input id="evaluation-dataset-description" value={desc} onChange={(event) => setDesc(event.target.value)} />
            {source === 'upload' ? <UploadField file={file} tooLarge={tooLarge} onFile={setFile} /> : null}
            {source === 'reuse' ? (
              <ReuseFields
                presets={presets}
                preset={preset}
                version={version}
                loading={query.isLoading}
                onPreset={(id) => {
                  setPresetId(id);
                  setVersionId('');
                }}
                onVersion={setVersionId}
              />
            ) : null}
            {importError ? <p className="text-sm text-destructive">{importError}</p> : null}
            {create.error && !importError ? (
              <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p>
            ) : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => downloadDatasetTemplate(locale)}>
                {msg('evaluations.downloadTemplate', 'Download template')}
              </Button>
              <Button
                type="submit"
                disabled={
                  create.isPending || (source === 'reuse' && version.length === 0) || (source === 'upload' && !file)
                }
              >
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

function DatasetSourcePicker({
  source,
  onSource,
}: {
  source: DatasetSource;
  onSource: (source: DatasetSource) => void;
}) {
  const { msg } = useI18n();
  const options: Array<{ id: DatasetSource; label: string }> = [
    { id: 'blank', label: msg('evaluations.createBlank', 'Empty dataset') },
    { id: 'upload', label: msg('evaluations.createUpload', 'Upload a file') },
    { id: 'reuse', label: msg('evaluations.createReuse', 'Reuse a preset') },
  ];
  return (
    <div className="flex flex-wrap gap-2">
      {options.map((option) => (
        <Button
          key={option.id}
          type="button"
          variant={source === option.id ? 'default' : 'outline'}
          aria-pressed={source === option.id}
          onClick={() => onSource(option.id)}
        >
          {option.label}
        </Button>
      ))}
    </div>
  );
}

function UploadField({
  file,
  tooLarge,
  onFile,
}: {
  file: File | null;
  tooLarge: boolean;
  onFile: (file: File | null) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <Label htmlFor="evaluation-dataset-file">{msg('evaluations.importFile', 'JSONL or ZIP file')}</Label>
      <Input
        id="evaluation-dataset-file"
        type="file"
        accept=".jsonl,.zip,application/jsonl,application/zip"
        onChange={(event) => onFile(event.target.files?.[0] ?? null)}
      />
      <p className="text-sm text-muted-foreground">
        {file?.name ??
          msg(
            'evaluations.importFileHint',
            'A JSONL file, or a ZIP up to 100MB with one JSONL plus optional input_files/ and reference_files/. Companion files are copied into workspace Files and stored on the cases as file_ids.',
          )}
      </p>
      {tooLarge ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.importFileTooLarge', 'File must be 100MB or smaller.')}
        </p>
      ) : null}
    </>
  );
}

function ReuseFields({
  presets,
  preset,
  version,
  loading,
  onPreset,
  onVersion,
}: {
  presets: EvaluationDatasetPreset[];
  preset: EvaluationDatasetPreset | undefined;
  version: string;
  loading: boolean;
  onPreset: (id: string) => void;
  onVersion: (id: string) => void;
}) {
  const { msg } = useI18n();
  if (!loading && presets.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">{msg('evaluations.noPresetsToReuse', 'No presets to reuse yet.')}</p>
    );
  }
  if (!preset) {
    return null;
  }
  const presetItems = presets.map((item) => ({ value: item.id, label: item.name }));
  const versionItems = preset.versions.map((item) => ({ value: item.id, label: `v${item.version}` }));
  return (
    <>
      <Select
        value={preset.id}
        items={presetItems}
        onValueChange={(next) => {
          if (next) {
            onPreset(next);
          }
        }}
      >
        <SelectTrigger aria-label={msg('evaluations.dataset', 'Dataset')} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {presetItems.map((item) => (
            <SelectItem key={item.value} value={item.value} label={item.label}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {versionItems.length > 1 ? (
        <Select
          value={version}
          items={versionItems}
          onValueChange={(next) => {
            if (next) {
              onVersion(next);
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
    </>
  );
}

function selectedVersion(preset: EvaluationDatasetPreset | undefined, versionId: string) {
  const versions = preset?.versions ?? [];
  if (versions.some((item) => item.id === versionId)) {
    return versionId;
  }
  return versions[versions.length - 1]?.id ?? '';
}

function datasetCreateInput(
  source: DatasetSource,
  name: string,
  desc: string,
  version: string,
): CreateEvaluationDatasetInput {
  if (source === 'reuse') {
    return { name, desc, source_version_id: version };
  }
  return { name, desc };
}
