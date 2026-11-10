import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState, type FormEvent } from 'react';

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
  const [error, setError] = useState('');
  useEffect(() => {
    if (!open) return;
    setQuery(item?.query ?? '');
    setExpected(item?.expected_answer ?? '');
    setCheckpoints(acceptanceItems(item?.acceptance ?? ''));
    setPolarity(item?.polarity ?? '');
    setError('');
  }, [open, item]);
  const save = useMutation({
    mutationFn: async () => {
      const body = {
        query,
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
    if (!query.trim()) {
      setError(msg('evaluations.queryRequired', 'Enter a query.'));
      return;
    }
    save.mutate();
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
            <Label htmlFor="evaluation-case-query">
              {msg('evaluations.query', 'Query')}
              <span className="text-destructive">*</span>
            </Label>
            <Textarea
              id="evaluation-case-query"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              rows={4}
              placeholder={msg('evaluations.queryPlaceholder', 'The question or task to score.')}
            />
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
            <Button type="submit" disabled={save.isPending}>
              {item ? msg('common.save', 'Save') : msg('evaluations.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
