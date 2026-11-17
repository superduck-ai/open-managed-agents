import { useMutation, useQueryClient } from '@tanstack/react-query';
import { FileUp } from 'lucide-react';
import { useRef, useState, type ChangeEvent, type DragEvent, type FormEvent } from 'react';

import { anthropicBetaApi } from '../../shared/api/anthropic';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription } from '../../shared/ui/alert';
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
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { Textarea } from '../../shared/ui/textarea';
import { createEvaluationDataset, evaluationError } from './api';
import { CaseGenerationFooter, CaseGenerationProgress, CaseGenerationStepView } from './CaseGenerationFlow';
import { CaseGenerationSetupFields } from './CaseGenerationSetupFields';
import { datasetWithUploadedFiles, missingCaseFileNames } from './caseFileRefs';
import { downloadBlob, downloadDatasetSample, type DatasetSampleKind } from './datasetSample';
import { readUploadedDataset, sampleWorkbook, type PendingImage } from './spreadsheetDataset';
import { useCaseGeneration } from './useCaseGeneration';
import { useEvaluationScope } from './useEvaluationScope';

type DatasetSource = 'upload' | 'paste' | 'generate';

export function DatasetCreateDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { locale, msg } = useI18n();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const [source, setSource] = useState<DatasetSource>('generate');
  const [name, setName] = useState('');
  const [pasteContent, setPasteContent] = useState('');
  const [uploadContent, setUploadContent] = useState('');
  const [readingFile, setReadingFile] = useState(false);
  const [pendingImages, setPendingImages] = useState<PendingImage[]>([]);
  const [error, setError] = useState('');
  const [nameFromFile, setNameFromFile] = useState('');
  const [fileLabel, setFileLabel] = useState('');
  const fileRead = useRef(0);
  const generation = useCaseGeneration({ open: open && source === 'generate', requireAgent: true });
  const generating = source === 'generate';
  const content = source === 'paste' ? pasteContent : uploadContent;
  const create = useMutation({
    mutationFn: async () => {
      const datasetName = datasetCreateName(
        source,
        name,
        nameFromFile,
        msg('evaluations.datasetNamePlaceholder', 'My dataset'),
      );
      if (generating) {
        return createEvaluationDataset(scope.orgUuid, scope.workspaceId, {
          name: datasetName,
          desc: '',
          cases: generation.selected,
        });
      }
      const readyContent =
        source === 'upload' ? await uploadPendingImages(content, pendingImages, scope.workspaceId) : content;
      return createEvaluationDataset(scope.orgUuid, scope.workspaceId, {
        name: datasetName,
        desc: '',
        content: readyContent,
      });
    },
    onSuccess: async () => {
      reset();
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function reset() {
    fileRead.current += 1;
    setName('');
    setPasteContent('');
    setUploadContent('');
    generation.reset();
    setNameFromFile('');
    setFileLabel('');
    setReadingFile(false);
    setPendingImages([]);
    setError('');
    setSource('generate');
  }

  function takeFile(file: File | undefined) {
    if (!file) return;
    const ticket = fileRead.current + 1;
    fileRead.current = ticket;
    setError('');
    setReadingFile(true);
    setFileLabel(file.name);
    setUploadContent('');
    setPendingImages([]);
    void readUploadedDataset(file).then((result) => {
      if (fileRead.current !== ticket) return;
      setReadingFile(false);
      if (result.error) {
        setFileLabel('');
        setNameFromFile('');
        const readError = uploadReadError(result.error);
        setError(msg(readError.id, readError.fallback));
        return;
      }
      setUploadContent(result.content);
      setPendingImages(result.images);
      const base = file.name.replace(/\.[^.]+$/, '');
      setNameFromFile(base);
    });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (generating && generation.step !== 'review') {
      setError('');
      generation.advance();
      return;
    }
    const message = generating ? '' : datasetFormError({ source, readingFile, content, name, pendingImages }, msg);
    if (message) {
      setError(message);
      return;
    }
    create.mutate();
  }

  const reviewing = generating && generation.step !== 'setup';
  const shownError = error || (generating ? generation.error : '');
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent
        className={`flex max-h-[calc(100vh-4rem)] flex-col gap-0 overflow-hidden p-0 ${
          reviewing ? 'sm:max-w-3xl' : 'sm:max-w-xl'
        }`}
        closeButtonDisabled={create.isPending || generation.busy}
      >
        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={onSubmit}>
          <DialogHeader className="px-6 pt-6 pr-12">
            <DialogTitle>{msg('evaluations.createDataset', 'Create dataset')}</DialogTitle>
            <DialogDescription>
              {msg('evaluations.datasetSourceHint', 'Upload a file, paste cases, or generate them from an agent.')}
            </DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 flex-1 content-start gap-6 overflow-y-auto px-6 py-6">
            {generating && generation.busy ? <CaseGenerationProgress generation={generation} /> : null}
            {reviewing ? (
              <CaseGenerationStepView generation={generation} disabled={create.isPending} />
            ) : (
              <Tabs value={source} onValueChange={(value) => setSource(datasetSource(value))}>
                <TabsList className="h-9">
                  <TabsTrigger className="px-3 data-active:border-foreground" value="generate">
                    {msg('evaluations.generate', 'Generate')}
                  </TabsTrigger>
                  <TabsTrigger className="px-3 data-active:border-foreground" value="upload">
                    {msg('evaluations.uploadFile', 'Upload')}
                  </TabsTrigger>
                  <TabsTrigger className="px-3 data-active:border-foreground" value="paste">
                    {msg('evaluations.pasteJson', 'Paste JSON')}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="generate" className="grid gap-5 pt-5">
                  <Label htmlFor="evaluation-dataset-name">{msg('evaluations.name', 'Name')}</Label>
                  <Input
                    id="evaluation-dataset-name"
                    value={name}
                    placeholder={msg('evaluations.datasetNamePlaceholder', 'My dataset')}
                    onChange={(event) => setName(event.target.value)}
                  />
                  <CaseGenerationSetupFields generation={generation} requireAgent disabled={generation.busy} />
                </TabsContent>
                <TabsContent value="upload" className="grid gap-5 pt-5">
                  <div className="grid gap-2">
                    <Label htmlFor="evaluation-upload-name">{msg('evaluations.name', 'Name')}</Label>
                    <Input
                      id="evaluation-upload-name"
                      value={name}
                      placeholder={msg('evaluations.datasetNamePlaceholder', 'My dataset')}
                      onChange={(event) => setName(event.target.value)}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label>{msg('evaluations.file', 'File')}</Label>
                    <DatasetFileDrop onFile={takeFile} fileLabel={fileLabel} />
                  </div>
                  <div className="grid gap-3">
                    <p className="text-sm text-muted-foreground">
                      {msg('evaluations.uploadNameHint', 'An empty name uses the file name.')}
                    </p>
                    <DatasetSampleDownloads />
                  </div>
                </TabsContent>
                <TabsContent value="paste" className="grid gap-5 pt-5">
                  <div className="grid gap-2">
                    <Label htmlFor="evaluation-paste-name">{msg('evaluations.name', 'Name')}</Label>
                    <Input
                      id="evaluation-paste-name"
                      value={name}
                      placeholder={msg('evaluations.datasetNamePlaceholder', 'My dataset')}
                      onChange={(event) => setName(event.target.value)}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="evaluation-paste-json">
                      {msg('evaluations.pasteData', 'Evaluation data (JSON)')}
                    </Label>
                    <Textarea
                      id="evaluation-paste-json"
                      className="min-h-40"
                      value={pasteContent}
                      onChange={(event) => setPasteContent(event.target.value)}
                      rows={8}
                      placeholder={
                        locale === 'zh-CN'
                          ? '[{"query":"示例问题","expected_answer":"期望回答"}]'
                          : '[{"query":"Example question","expected_answer":"Expected answer"}]'
                      }
                    />
                  </div>
                  <p className="text-sm text-muted-foreground">
                    {msg(
                      'evaluations.pasteHint',
                      'Each item needs a question. You can also add the expected answer and the tools to call. Pasted text cannot contain new pictures. Use inputs to reference pictures or documents already in the file library.',
                    )}
                  </p>
                </TabsContent>
              </Tabs>
            )}
          </div>
          {shownError ? (
            <Alert variant="destructive" className="mx-6 mb-2">
              <AlertDescription>{shownError}</AlertDescription>
            </Alert>
          ) : null}
          {generating ? (
            <CaseGenerationFooter
              generation={generation}
              saving={create.isPending}
              finishLabel={msg('evaluations.createWithDrafts', 'Create dataset with {count} cases', {
                count: generation.selected.length,
              })}
            />
          ) : (
            <DialogFooter className="border-t border-border px-6 py-4">
              <DialogClose render={<Button type="button" variant="outline" disabled={create.isPending} />}>
                {msg('common.cancel', 'Cancel')}
              </DialogClose>
              <Button type="submit" disabled={create.isPending}>
                {datasetSubmitLabel(source, msg)}
              </Button>
            </DialogFooter>
          )}
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DatasetFileDrop({ onFile, fileLabel }: { onFile: (file: File | undefined) => void; fileLabel: string }) {
  const { msg } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);

  function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragging(false);
    onFile(event.dataTransfer.files[0]);
  }

  return (
    <div
      className={`grid min-h-44 place-items-center rounded-xl border border-dashed px-6 py-8 text-center ${
        dragging ? 'border-primary bg-accent' : 'border-border bg-muted/40'
      }`}
      onClick={() => inputRef.current?.click()}
      onDragEnter={(event) => {
        event.preventDefault();
        setDragging(true);
      }}
      onDragOver={(event) => {
        event.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
    >
      <input
        ref={inputRef}
        type="file"
        className="sr-only"
        accept=".json,.jsonl,.csv,.xlsx,application/json,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
        aria-label={msg('evaluations.uploadFile', 'Upload')}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onFile(event.target.files?.[0])}
      />
      <div className="grid justify-items-center gap-3">
        <FileUp className="size-8 text-primary" aria-hidden />
        {fileLabel ? (
          <p className="text-sm font-medium text-foreground">{fileLabel}</p>
        ) : (
          <p className="text-sm font-medium text-foreground">
            {msg('evaluations.dropLead', 'Click or drag')}{' '}
            <button
              type="button"
              className="text-primary hover:underline"
              onClick={(event) => {
                event.stopPropagation();
                inputRef.current?.click();
              }}
            >
              {msg('evaluations.uploadFile', 'Upload')}
            </button>
          </p>
        )}
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.uploadHint', 'JSON, JSONL, or CSV. Include a query column.')}
        </p>
      </div>
    </div>
  );
}

function DatasetSampleDownloads() {
  const { locale, msg } = useI18n();
  const chinese = locale.toLowerCase().startsWith('zh');
  const samples: Array<{ kind: DatasetSampleKind | 'xlsx'; label: string }> = [
    { kind: 'json', label: msg('evaluations.sampleJson', 'JSON sample') },
    { kind: 'jsonl', label: msg('evaluations.sampleJsonl', 'JSONL sample') },
    { kind: 'csv', label: msg('evaluations.sampleCsv', 'CSV sample') },
    { kind: 'xlsx', label: msg('evaluations.sampleXlsx', 'Excel sample') },
  ];
  return (
    <div className="grid gap-2">
      <p className="text-sm text-muted-foreground">{msg('evaluations.sampleFiles', 'Sample files')}</p>
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.samplePictureHint',
          'Only Excel (xlsx) can contain pictures in cells. CSV, JSON, and JSONL are text only.',
        )}
      </p>
      <div className="flex flex-wrap gap-6">
        {samples.map((sample) => (
          <button
            key={sample.kind}
            type="button"
            className="text-sm text-primary hover:underline"
            onClick={() =>
              sample.kind === 'xlsx' ? downloadSampleWorkbook() : downloadDatasetSample(sample.kind, chinese)
            }
          >
            {sample.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function downloadSampleWorkbook() {
  downloadBlob(
    new Blob([sampleWorkbook().buffer as ArrayBuffer], {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    }),
    'evaluation-dataset.sample.xlsx',
  );
}

export function datasetCreateName(source: DatasetSource, name: string, fileName: string, generateName: string) {
  const trimmed = name.trim();
  if (trimmed) return trimmed;
  if (source === 'upload') return fileName;
  if (source === 'generate') return generateName;
  return '';
}

function datasetFormError(
  input: {
    source: DatasetSource;
    readingFile: boolean;
    content: string;
    name: string;
    pendingImages: PendingImage[];
  },
  msg: (id: string, fallback: string) => string,
) {
  const message = importSubmitMessage(input);
  if (message) return msg(message.id, message.fallback);
  if (missingCaseFileNames(input.content, input.pendingImages).length === 0) return '';
  return msg(
    'evaluations.imageNotInFile',
    'This file does not contain the picture. Put the picture in an Excel cell and upload the xlsx file.',
  );
}

function importSubmitMessage(input: { source: DatasetSource; readingFile: boolean; content: string; name: string }) {
  if (input.readingFile) return { id: 'evaluations.fileReading', fallback: 'The file is still loading.' };
  if (!input.content.trim()) {
    return { id: 'evaluations.importRequired', fallback: 'Add evaluation data before creating the dataset.' };
  }
  if (input.source === 'paste' && !input.name.trim())
    return { id: 'evaluations.nameRequired', fallback: 'Enter a name.' };
  return null;
}

function datasetSource(value: unknown): DatasetSource {
  if (value === 'paste' || value === 'upload') return value;
  return 'generate';
}

function datasetSubmitLabel(source: DatasetSource, msg: (id: string, fallback: string) => string) {
  if (source === 'upload') return msg('evaluations.uploadAndCreate', 'Upload and create');
  return msg('evaluations.create', 'Create');
}

function uploadReadError(error: 'xls' | 'csv-image' | 'read') {
  if (error === 'xls') {
    return {
      id: 'evaluations.xlsUnsupported',
      fallback: 'The older xls format has no readable cell pictures. Save the workbook as xlsx.',
    };
  }
  if (error === 'csv-image') {
    return {
      id: 'evaluations.imageNotInFile',
      fallback: 'This file does not contain the picture. Put the picture in an Excel cell and upload the xlsx file.',
    };
  }
  return { id: 'evaluations.fileReadFailed', fallback: 'Could not read this file. Choose it again.' };
}

async function uploadPendingImages(content: string, images: PendingImage[], workspaceId: string) {
  if (images.length === 0) return content;
  const uploaded: Record<string, { id: string; kind: 'image' | 'document' }> = {};
  for (const image of images) {
    const bytes = image.bytes.slice();
    const file = new File([bytes.buffer], image.name, { type: image.mime });
    const result = await anthropicBetaApi.files.upload<{ id: string }>(file, workspaceId);
    uploaded[image.name] = { id: result.id, kind: image.kind };
  }
  return datasetWithUploadedFiles(content, uploaded);
}
