import { Plus, Trash2 } from 'lucide-react';
import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationErrorMessage, type EvaluationCase, type UpdateEvaluationCaseInput } from './api';
import { evaluationCaseFiles, evaluationCaseJSON } from './caseFiles';
import { checkpointsFieldValue, parseCheckpointsField } from './sessionCase';
import { CheckpointsField } from './SessionCaseFields';
import { useDeleteEvaluationCase, useUpdateEvaluationCase } from './useEvaluationDatasets';

export function DatasetVersionEditor({
  open,
  version,
  cases,
  datasetId,
  onOpenChange,
  onAdd,
}: {
  open: boolean;
  version: number;
  cases: EvaluationCase[];
  datasetId: string;
  onOpenChange: (open: boolean) => void;
  onAdd: () => void;
}) {
  const { msg } = useI18n();
  const [selectedId, setSelectedId] = useState(cases[0]?.id ?? '');
  const selected = cases.find((item) => item.id === selectedId) ?? cases[0] ?? null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(860px,90vh)] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{msg('evaluations.editVersion', 'Edit version v{version}', { version })}</DialogTitle>
        </DialogHeader>
        <div className="grid items-start gap-4 md:grid-cols-[220px_1fr]">
          <aside className="rounded-lg border">
            <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
              <p className="text-sm font-medium">
                {msg('evaluations.casesInVersion', 'Cases ({count})', { count: cases.length })}
              </p>
              <Button type="button" variant="ghost" size="sm" onClick={onAdd}>
                <Plus className="size-4" aria-hidden />
                {msg('evaluations.addShort', 'Add')}
              </Button>
            </div>
            <ul className="max-h-96 overflow-y-auto p-2">
              {cases.map((item) => {
                const active = item.id === selected?.id;
                return (
                  <li key={item.id}>
                    <button
                      type="button"
                      className={`w-full truncate rounded-md px-2 py-1.5 text-left text-sm ${active ? 'bg-muted font-medium' : 'hover:bg-muted/60'}`}
                      onClick={() => setSelectedId(item.id)}
                    >
                      {item.name}
                    </button>
                  </li>
                );
              })}
            </ul>
          </aside>
          {selected ? (
            <CaseDraft key={selected.id} item={selected} datasetId={datasetId} onClose={() => onOpenChange(false)} />
          ) : (
            <p className="text-sm text-muted-foreground">{msg('evaluations.noCases', 'No cases yet.')}</p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function CaseDraft({ item, datasetId, onClose }: { item: EvaluationCase; datasetId: string; onClose: () => void }) {
  const { msg } = useI18n();
  const update = useUpdateEvaluationCase(datasetId);
  const remove = useDeleteEvaluationCase(datasetId);
  const [name, setName] = useState(item.name);
  const [desc, setDesc] = useState(item.desc);
  const [turns, setTurns] = useState(item.turns.length > 0 ? item.turns : ['']);
  const [checkpoints, setCheckpoints] = useState(checkpointsFieldValue(item.checkpoints));
  const [showJSON, setShowJSON] = useState(false);
  const [checkpointError, setCheckpointError] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const files = evaluationCaseFiles(item.input_files);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    const parsed = caseUpdateInput({
      name,
      desc,
      turns,
      checkpoints,
      inputFiles: item.input_files,
      referenceFiles: item.reference_files,
    });
    setCheckpointError(parsed.checkpointError);
    if (!parsed.input) {
      return;
    }
    await update.mutateAsync({ caseId: item.id, body: parsed.input });
  }

  return (
    <form className="grid gap-4" onSubmit={(event) => void onSubmit(event)}>
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm font-medium">{msg('evaluations.caseTitle', 'Name')}</p>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          onClick={() => setShowJSON((value) => !value)}
        >
          {msg('evaluations.viewJSON', 'View JSON')}
        </Button>
      </div>
      {showJSON ? (
        <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-5">
          {evaluationCaseJSON({ name, desc, turns, checkpoints: item.checkpoints, input_files: item.input_files })}
        </pre>
      ) : null}
      <Label htmlFor="evaluation-version-case-name">{msg('evaluations.caseTitle', 'Name')}</Label>
      <Input
        id="evaluation-version-case-name"
        value={name}
        onChange={(event) => setName(event.target.value)}
        required
      />
      <Label htmlFor="evaluation-version-case-desc">{msg('evaluations.descriptionField', 'Description')}</Label>
      <Textarea id="evaluation-version-case-desc" value={desc} onChange={(event) => setDesc(event.target.value)} />
      <div className="grid gap-2">
        <p className="text-sm font-medium">{msg('evaluations.caseInput', 'Input')}</p>
        {turns.map((turn, index) => (
          <div key={index} className="grid gap-1">
            <Label htmlFor={`evaluation-version-turn-${index}`}>
              {msg('evaluations.turnLabel', 'Turn {n}', { n: index + 1 })}
            </Label>
            <Textarea
              id={`evaluation-version-turn-${index}`}
              value={turn}
              onChange={(event) =>
                setTurns(turns.map((value, turnIndex) => (turnIndex === index ? event.target.value : value)))
              }
              required={index === 0}
            />
          </div>
        ))}
        <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => setTurns([...turns, ''])}>
          <Plus className="size-4" aria-hidden />
          {msg('evaluations.addTurn', 'Add turn')}
        </Button>
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.turnHint', 'Each box is one turn. A turn can contain multiple lines.')}
        </p>
      </div>
      <CheckpointsField
        id="evaluation-version-case-checkpoints"
        value={checkpoints}
        onChange={setCheckpoints}
        error={checkpointError}
      />
      <div className="grid gap-2">
        <p className="text-sm font-medium">{msg('evaluations.inputAttachments', 'Input attachments')}</p>
        {files.length === 0 ? (
          <Input value="" readOnly aria-label={msg('evaluations.inputAttachments', 'Input attachments')} />
        ) : (
          files.map((file) => (
            <Input key={`${file.fileId}:${file.name}`} value={file.name} readOnly aria-label={file.name} />
          ))
        )}
      </div>
      {update.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(update.error)}</p> : null}
      <Button
        type="button"
        variant="outline"
        className="w-fit"
        onClick={() => setConfirmDelete(true)}
        disabled={remove.isPending}
      >
        <Trash2 className="size-4" aria-hidden />
        {msg('evaluations.deleteCase', 'Delete case')}
      </Button>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button type="submit" disabled={update.isPending}>
          {msg('evaluations.saveCase', 'Save')}
        </Button>
      </DialogFooter>
      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{msg('evaluations.deleteCaseTitle', 'Delete this case?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {msg('evaluations.deleteCaseConfirm', 'This removes the case from the current dataset version.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() =>
                void remove.mutateAsync(item.id).then(() => {
                  setConfirmDelete(false);
                })
              }
            >
              {msg('common.delete', 'Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}

function caseUpdateInput(draft: {
  name: string;
  desc: string;
  turns: string[];
  checkpoints: string;
  inputFiles: unknown;
  referenceFiles: unknown;
}): { input: UpdateEvaluationCaseInput | null; checkpointError: boolean } {
  const parsedCheckpoints = parseCheckpointsField(draft.checkpoints);
  if (parsedCheckpoints.error) {
    return { input: null, checkpointError: true };
  }
  const turns = draft.turns.map((turn) => turn.trim()).filter((turn) => turn.length > 0);
  return {
    checkpointError: false,
    input: {
      name: draft.name,
      desc: draft.desc,
      turns,
      checkpoints: parsedCheckpoints.checkpoints,
      input_files: draft.inputFiles ?? undefined,
      reference_files: draft.referenceFiles ?? undefined,
    },
  };
}
