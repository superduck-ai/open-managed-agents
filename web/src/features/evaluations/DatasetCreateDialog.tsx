import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FileUp } from 'lucide-react';
import { useRef, useState, type ChangeEvent, type DragEvent, type FormEvent } from 'react';

import { listAgentVersions, listAgents } from '../managed-agents/api';
import { useI18n } from '../../shared/i18n';
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { Textarea } from '../../shared/ui/textarea';
import { createEvaluationDataset, evaluationError, generateEvaluationCases } from './api';
import { downloadDatasetSample, type DatasetSampleKind } from './datasetSample';
import { useEvaluationScope } from './useEvaluationScope';

type DatasetSource = 'upload' | 'paste' | 'generate';

export function DatasetCreateDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const [source, setSource] = useState<DatasetSource>('generate');
  const [name, setName] = useState('');
  const [content, setContent] = useState('');
  const [count, setCount] = useState('20');
  const [note, setNote] = useState('');
  const [agentId, setAgentId] = useState('');
  const [version, setVersion] = useState('');
  const [error, setError] = useState('');
  const versions = useQuery({
    queryKey: ['evaluation-dataset-agent-versions', scope.workspaceId, agentId],
    queryFn: () => listAgentVersions(agentId, scope.workspaceId),
    enabled: open && scope.ready && source === 'generate' && Boolean(agentId),
  });
  const [nameFromFile, setNameFromFile] = useState('');
  const [fileLabel, setFileLabel] = useState('');
  const agents = useQuery({
    queryKey: ['evaluation-agents', scope.workspaceId],
    queryFn: () => listAgents(scope.workspaceId),
    enabled: open && scope.ready && source === 'generate',
  });
  const create = useMutation({
    mutationFn: () =>
      submitDataset(scope.orgUuid, scope.workspaceId, {
        name: name.trim() || (source === 'upload' ? nameFromFile : source === 'generate' ? '我的评测集' : ''),
        source,
        content,
        count,
        note,
        agentId,
      }),
    onSuccess: async () => {
      reset();
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function reset() {
    setName('');
    setContent('');
    setCount('20');
    setNote('');
    setAgentId('');
    setVersion('');
    setNameFromFile('');
    setFileLabel('');
    setError('');
    setSource('generate');
  }

  function takeFile(file: File | undefined) {
    if (!file) return;
    const lower = file.name.toLowerCase();
    if (lower.endsWith('.xlsx') || lower.endsWith('.xls')) {
      setError(msg('evaluations.excelUnsupported', 'Excel is not supported yet. Use JSON, JSONL, or CSV.'));
      return;
    }
    setError('');
    setFileLabel(file.name);
    void file.text().then((text) => {
      setContent(text);
      const base = file.name.replace(/\.[^.]+$/, '');
      setNameFromFile(base);
      setName((current) => current || base);
    });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const amount = Number(count);
    if (source === 'generate' && (!Number.isInteger(amount) || amount < 1 || amount > 50)) {
      setError(msg('evaluations.generateCountInvalid', 'Generate between 1 and 50 cases.'));
      return;
    }
    if (source === 'generate' && !agentId) {
      setError(msg('evaluations.chooseAgent', 'Choose an agent'));
      return;
    }
    if (source !== 'upload' && source !== 'generate' && !name.trim()) {
      setError(msg('evaluations.name', 'Name'));
      return;
    }
    create.mutate();
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent className="gap-6 p-6 sm:max-w-xl" closeButtonDisabled={create.isPending}>
        <form className="grid gap-6" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{msg('evaluations.createDataset', 'Create dataset')}</DialogTitle>
            <DialogDescription>
              {msg('evaluations.datasetSourceHint', 'Upload a file, paste cases, or generate them from an agent.')}
            </DialogDescription>
          </DialogHeader>
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
              <Label>{msg('evaluations.agentRequired', 'Agent')}</Label>
              <Select
                value={agentId || null}
                items={(agents.data?.data ?? []).map((agent) => ({ value: agent.id, label: agent.name }))}
                onValueChange={(value) => {
                  setAgentId(value ?? '');
                  setVersion('');
                }}
              >
                <SelectTrigger className="w-full" aria-label={msg('evaluations.agentRequired', 'Agent')}>
                  <SelectValue placeholder={msg('evaluations.chooseAgent', 'Choose an agent')} />
                </SelectTrigger>
                <SelectContent>
                  {(agents.data?.data ?? []).map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Label>{msg('evaluations.versionRequired', 'Version')}</Label>
              <Select
                value={version || null}
                items={(versions.data?.data ?? []).map((item) => ({
                  value: String(item.version),
                  label: `v${item.version}`,
                }))}
                onValueChange={(value) => setVersion(value ?? '')}
              >
                <SelectTrigger className="w-full" aria-label={msg('evaluations.versionRequired', 'Version')}>
                  <SelectValue placeholder={msg('evaluations.chooseAgentFirst', 'Choose an agent first')} />
                </SelectTrigger>
                <SelectContent>
                  {(versions.data?.data ?? []).map((item) => (
                    <SelectItem key={item.version} value={String(item.version)}>
                      v{item.version}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Label>{msg('evaluations.generateCount', 'How many')}</Label>
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setCount(String(Math.max(1, Number(count) - 1)))}
                >
                  −
                </Button>
                <span className="w-8 text-center tabular-nums">{count}</span>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setCount(String(Math.min(50, Number(count) + 1)))}
                >
                  +
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                {msg(
                  'evaluations.generateCountHint',
                  '1–50. Cover each skill or tool at least once. About half should be normal paths, the rest edge cases.',
                )}
              </p>
              <Label htmlFor="evaluation-generate-note">{msg('evaluations.generateNote', 'What to cover')}</Label>
              <Textarea
                id="evaluation-generate-note"
                value={note}
                onChange={(event) => setNote(event.target.value)}
                rows={4}
                placeholder={msg(
                  'evaluations.generateNotePlaceholder',
                  'For example: the user is an overseas customer asking in English. Cover the refund flow.',
                )}
              />
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
                <Label htmlFor="evaluation-paste-json">{msg('evaluations.pasteData', 'Evaluation data (JSON)')}</Label>
                <Textarea
                  id="evaluation-paste-json"
                  className="min-h-40"
                  value={content}
                  onChange={(event) => setContent(event.target.value)}
                  rows={8}
                  placeholder={'[{"query":"示例问题","expectedAnswer":"标准答案","expectedToolTrace":[]}]'}
                />
              </div>
              <p className="text-sm text-muted-foreground">
                {msg(
                  'evaluations.pasteHint',
                  'Each item needs query. expectedAnswer and expectedToolTrace are optional.',
                )}
              </p>
            </TabsContent>
          </Tabs>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={create.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={create.isPending}>
              {datasetSubmitLabel(source, msg)}
            </Button>
          </DialogFooter>
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
      className={`grid min-h-44 place-items-center rounded-xl border border-dashed px-6 py-8 text-center ${dragging ? 'border-blue-500 bg-blue-50' : 'border-blue-200 bg-slate-50'}`}
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
        accept=".json,.jsonl,.csv,.xlsx,.xls,application/json,text/csv"
        aria-label={msg('evaluations.uploadFile', 'Upload')}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onFile(event.target.files?.[0])}
      />
      <div className="grid justify-items-center gap-3">
        <FileUp className="size-8 text-blue-500" aria-hidden />
        {fileLabel ? (
          <p className="text-sm font-medium text-foreground">{fileLabel}</p>
        ) : (
          <p className="text-sm font-medium text-foreground">
            {msg('evaluations.dropLead', 'Click or drag')}{' '}
            <button
              type="button"
              className="text-blue-500 hover:underline"
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
          {msg('evaluations.uploadHint', 'JSON, CSV, or Excel. Include a query column.')}
        </p>
      </div>
    </div>
  );
}

function DatasetSampleDownloads() {
  const { locale, msg } = useI18n();
  const chinese = locale.toLowerCase().startsWith('zh');
  const samples: Array<{ kind: DatasetSampleKind; label: string }> = [
    { kind: 'json', label: msg('evaluations.sampleJson', 'JSON sample') },
    { kind: 'jsonl', label: msg('evaluations.sampleJsonl', 'JSONL sample') },
    { kind: 'csv', label: msg('evaluations.sampleCsv', 'CSV sample') },
  ];
  return (
    <div className="grid gap-2">
      <p className="text-sm text-muted-foreground">{msg('evaluations.sampleFiles', 'Sample files')}</p>
      <div className="flex flex-wrap gap-6">
        {samples.map((sample) => (
          <button
            key={sample.kind}
            type="button"
            className="text-sm text-blue-500 hover:underline"
            onClick={() => downloadDatasetSample(sample.kind, chinese)}
          >
            {sample.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function datasetSource(value: unknown): DatasetSource {
  if (value === 'paste' || value === 'upload') return value;
  return 'generate';
}

function datasetSubmitLabel(source: DatasetSource, msg: (id: string, fallback: string) => string) {
  if (source === 'generate') return msg('evaluations.startGenerate', 'Start generating');
  if (source === 'upload') return msg('evaluations.uploadAndCreate', 'Upload and create');
  return msg('evaluations.create', 'Create');
}

async function submitDataset(
  orgUuid: string,
  workspaceId: string,
  input: {
    name: string;
    source: DatasetSource;
    content: string;
    count: string;
    note: string;
    agentId: string;
  },
) {
  const dataset = await createEvaluationDataset(orgUuid, workspaceId, {
    name: input.name,
    desc: '',
    content: input.source === 'generate' ? undefined : input.content,
  });
  if (input.source !== 'generate') return dataset;
  await generateEvaluationCases(orgUuid, workspaceId, dataset.id, {
    count: Number(input.count),
    note: input.note,
    agent_id: input.agentId || undefined,
  });
  return dataset;
}
