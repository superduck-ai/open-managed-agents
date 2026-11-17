import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { createEvaluationCases, evaluationError } from './api';
import { CaseGenerationFooter, CaseGenerationProgress, CaseGenerationStepView } from './CaseGenerationFlow';
import { CaseGenerationSetupFields } from './CaseGenerationSetupFields';
import { useCaseGeneration } from './useCaseGeneration';
import { useEvaluationScope } from './useEvaluationScope';

export function CaseGenerateDialog({
  open,
  onOpenChange,
  datasetId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  datasetId: string;
}) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const generation = useCaseGeneration({ open, datasetId, requireAgent: false });
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => createEvaluationCases(scope.orgUuid, scope.workspaceId, datasetId, generation.selected),
    onSuccess: async () => {
      close();
      await queryClient.invalidateQueries({
        queryKey: ['evaluation-cases', scope.orgUuid, scope.workspaceId, datasetId],
      });
      await queryClient.invalidateQueries({
        queryKey: ['evaluation-dataset', scope.orgUuid, scope.workspaceId, datasetId],
      });
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function close() {
    generation.reset();
    setError('');
    onOpenChange(false);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError('');
    if (generation.step === 'review') {
      save.mutate();
      return;
    }
    generation.advance();
  }

  const shownError = error || generation.error;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          close();
          return;
        }
        onOpenChange(next);
      }}
    >
      <DialogContent
        className={`flex max-h-[calc(100vh-4rem)] flex-col gap-0 overflow-hidden p-0 ${
          generation.step === 'setup' ? 'sm:max-w-xl' : 'sm:max-w-3xl'
        }`}
        closeButtonDisabled={save.isPending || generation.busy}
      >
        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={onSubmit}>
          <DialogHeader className="px-6 pt-6 pr-12">
            <DialogTitle>{msg('evaluations.generateCases', 'Generate cases')}</DialogTitle>
            <DialogDescription>
              {msg(
                'evaluations.generateCasesHint',
                'New cases build on the ones already in this dataset. Repeated questions are removed.',
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 flex-1 content-start gap-5 overflow-y-auto px-6 py-6">
            <CaseGenerationProgress generation={generation} />
            {generation.step === 'setup' ? (
              <CaseGenerationSetupFields generation={generation} requireAgent={false} disabled={generation.busy} />
            ) : (
              <CaseGenerationStepView generation={generation} disabled={save.isPending} />
            )}
          </div>
          {shownError ? (
            <Alert variant="destructive" className="mx-6 mb-2">
              <AlertDescription>{shownError}</AlertDescription>
            </Alert>
          ) : null}
          <CaseGenerationFooter
            generation={generation}
            saving={save.isPending}
            finishLabel={msg('evaluations.addDrafts', 'Add {count} cases', { count: generation.selected.length })}
          />
        </form>
      </DialogContent>
    </Dialog>
  );
}
