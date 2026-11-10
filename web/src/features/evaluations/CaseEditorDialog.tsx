import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from 'react';

import { anthropicBetaApi } from '../../shared/api/anthropic';
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
import { Label } from '../../shared/ui/label';
import { Tabs, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { Textarea } from '../../shared/ui/textarea';
import { AcceptanceChecklist } from './AcceptanceChecklist';
import { createEvaluationCases, evaluationError, updateEvaluationCase, type EvaluationCase } from './api';
import { acceptanceItems, joinAcceptance } from './caseChecklist';
import { blockTypeForMime, buildInputs, fileBlocks, textFromInputs, type CaseFile } from './caseInputs';

export function CaseEditorDialog({
  open,
  onOpenChange,
  orgUuid,
  workspaceId,
  datasetId,
  item,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgUuid: string;
  workspaceId: string;
  datasetId: string;
  item: EvaluationCase | null;
}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState('');
  const [expected, setExpected] = useState('');
  const [checkpoints, setCheckpoints] = useState<string[]>(['']);
  const [polarity, setPolarity] = useState('');
  const [files, setFiles] = useState<CaseFile[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!open) return;
    const inputs = item?.inputs ?? [];
    setQuery(textFromInputs(inputs) || item?.query || '');
    setFiles(fileBlocks(inputs));
    setExpected(item?.expected_answer ?? '');
    setCheckpoints(acceptanceItems(item?.acceptance ?? ''));
    setPolarity(item?.polarity ?? '');
    setError('');
  }, [open, item]);
  const save = useMutation({
    mutationFn: async () => {
      const body = {
        query,
        inputs: buildInputs(query, files),
        expected_answer: expected,
        acceptance: polarity === 'negative' ? joinAcceptance(checkpoints) : '',
        polarity,
      };
      if (item) {
        await updateEvaluationCase(orgUuid, workspaceId, datasetId, item.id, {
          ...body,
          expected_tool_trace: item.expected_tool_trace,
          source_session_id: item.source_session_id,
        });
        return;
      }
      await createEvaluationCases(orgUuid, workspaceId, datasetId, [body]);
    },
    onSuccess: async () => {
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-cases', orgUuid, workspaceId, datasetId] });
      await queryClient.invalidateQueries({ queryKey: ['evaluation-dataset', orgUuid, workspaceId, datasetId] });
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', orgUuid, workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (buildInputs(query, files).length === 0) {
      setError(msg('evaluations.inputRequired', 'Enter a query or attach a file.'));
      return;
    }
    save.mutate();
  }

  async function onFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    const kind = blockTypeForMime(file.type);
    if (!kind) {
      setError(msg('evaluations.fileMedia', 'Use a PNG, JPEG, GIF, WebP, PDF, or text file.'));
      return;
    }
    setUploading(true);
    try {
      const uploaded = await anthropicBetaApi.files.upload<{ id: string }>(file, workspaceId);
      setFiles((current) => [...current, { id: uploaded.id, kind }]);
      setError('');
    } catch (cause) {
      setError(evaluationError(cause));
    } finally {
      setUploading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-h-[calc(100vh-4rem)] gap-5 overflow-y-auto p-6 sm:max-w-xl"
        closeButtonDisabled={save.isPending}
      >
        <form className="grid gap-5" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>
              {item ? msg('evaluations.editCase', 'Edit case') : msg('evaluations.addCase', 'Add case')}
            </DialogTitle>
            <DialogDescription>
              {item
                ? msg('evaluations.editCaseHint', 'Change the query, expected answer, and label.')
                : msg('evaluations.addCaseHint', 'Add one case for scoring the agent.')}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="evaluation-case-query">{msg('evaluations.query', 'Query')}</Label>
            <Textarea
              id="evaluation-case-query"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              rows={4}
              placeholder={msg('evaluations.queryPlaceholder', 'The question or task to score.')}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="outline" disabled={uploading} onClick={() => fileInput.current?.click()}>
                {msg('evaluations.attachFile', 'Add image or document')}
              </Button>
              <input
                ref={fileInput}
                type="file"
                className="hidden"
                accept="image/jpeg,image/png,image/gif,image/webp,application/pdf,text/plain"
                onChange={(event) => void onFile(event)}
              />
              {files.map((file) => (
                <span key={file.id} className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs">
                  {file.id}
                  <button
                    type="button"
                    className="text-muted-foreground"
                    onClick={() => setFiles((current) => current.filter((item) => item.id !== file.id))}
                  >
                    {msg('evaluations.removeFile', 'Remove')}
                  </button>
                </span>
              ))}
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="evaluation-case-expected">
              {msg('evaluations.expectedOptional', 'Expected answer, optional')}
            </Label>
            <Textarea
              id="evaluation-case-expected"
              value={expected}
              onChange={(event) => setExpected(event.target.value)}
              rows={3}
              placeholder={msg(
                'evaluations.expectedPlaceholder',
                'Describe the ideal answer. Leave empty to score by the rubric only.',
              )}
            />
          </div>
          <div className="grid gap-2">
            <Label>{msg('evaluations.polarity', 'Label')}</Label>
            <Tabs
              value={polarity || 'none'}
              onValueChange={(value) => setPolarity(value === 'none' ? '' : String(value))}
            >
              <TabsList className="h-9">
                <TabsTrigger className="px-3 data-active:border-foreground" value="none">
                  {msg('evaluations.unlabeled', 'No label')}
                </TabsTrigger>
                <TabsTrigger className="px-3 data-active:border-foreground" value="positive">
                  {msg('evaluations.positive', 'Positive')}
                </TabsTrigger>
                <TabsTrigger className="px-3 data-active:border-foreground" value="negative">
                  {msg('evaluations.negative', 'Negative')}
                </TabsTrigger>
              </TabsList>
            </Tabs>
            <p className="text-sm text-muted-foreground">
              {msg(
                'evaluations.polarityHint',
                'Acceptance checks are only kept for a negative example. They say what a fix must do.',
              )}
            </p>
          </div>
          {polarity === 'negative' ? <AcceptanceChecklist items={checkpoints} onChange={setCheckpoints} /> : null}
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={save.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={save.isPending || uploading}>
              {item ? msg('common.save', 'Save') : msg('evaluations.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
