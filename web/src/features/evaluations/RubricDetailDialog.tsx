import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationError, saveEvaluationRubricVersion, type EvaluationRubric } from './api';

export function RubricDetailDialog({
  rubric,
  onOpenChange,
  orgUuid,
  workspaceId,
}: {
  rubric: EvaluationRubric | null;
  onOpenChange: (open: boolean) => void;
  orgUuid: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const [body, setBody] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    setBody(rubric?.body ?? '');
    setError('');
  }, [rubric]);
  const save = useMutation({
    mutationFn: () =>
      saveEvaluationRubricVersion(orgUuid, workspaceId, rubric?.id ?? '', {
        body,
        pass_score: rubric?.pass_score ?? 60,
      }),
    onSuccess: async () => {
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-rubrics', orgUuid, workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!rubric) return;
    save.mutate();
  }

  return (
    <Dialog open={rubric !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" closeButtonDisabled={save.isPending}>
        <form className="grid gap-3" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{rubric?.name || msg('evaluations.rubrics', 'Rubrics')}</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            {msg('evaluations.version', 'Version')} v{rubric?.version ?? 1} ·{' '}
            {msg('evaluations.passScore', 'Pass score')} {rubric?.pass_score ?? 60}
          </p>
          <Label htmlFor="evaluation-rubric-edit">{msg('evaluations.rubricBody', 'Rubric')}</Label>
          <Textarea
            id="evaluation-rubric-edit"
            value={body}
            onChange={(event) => setBody(event.target.value)}
            rows={12}
            required
          />
          <p className="text-xs text-muted-foreground">
            {msg('evaluations.rubricVersionHint', 'Saving adds a new version. Tasks already running keep the old one.')}
          </p>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={save.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={save.isPending || !body.trim()}>
              {msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
