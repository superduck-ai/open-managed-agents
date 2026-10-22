import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { downloadApiFile } from '../../shared/api/file-download';
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
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationErrorMessage, type EvaluationCase, type UpdateEvaluationCaseInput } from './api';
import { evaluationCaseFiles, evaluationCaseJSON, type EvaluationCaseFile } from './caseFiles';
import { useDeleteEvaluationCase, useUpdateEvaluationCase } from './useEvaluationDatasets';

export function CaseDrawer({
  item,
  workspaceId,
  datasetId,
  open,
  onOpenChange,
  onSaved,
  onDeleted,
}: {
  item: EvaluationCase | null;
  workspaceId: string;
  datasetId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: (item: EvaluationCase) => void;
  onDeleted: () => void;
}) {
  const { msg } = useI18n();
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="overflow-y-auto">
        <SheetHeader>
          <SheetTitle>{item?.name ?? msg('evaluations.caseDetail', 'Case')}</SheetTitle>
          <SheetDescription>
            {msg('evaluations.caseReadOnly', 'Saving updates this case on the current dataset version.')}
          </SheetDescription>
        </SheetHeader>
        {item ? (
          <CaseEditor
            key={item.id}
            item={item}
            workspaceId={workspaceId}
            datasetId={datasetId}
            onSaved={onSaved}
            onDeleted={onDeleted}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function CaseEditor({
  item,
  workspaceId,
  datasetId,
  onSaved,
  onDeleted,
}: {
  item: EvaluationCase;
  workspaceId: string;
  datasetId: string;
  onSaved: (item: EvaluationCase) => void;
  onDeleted: () => void;
}) {
  const { msg } = useI18n();
  const update = useUpdateEvaluationCase(datasetId);
  const remove = useDeleteEvaluationCase(datasetId);
  const [name, setName] = useState(item.name);
  const [desc, setDesc] = useState(item.desc);
  const [turns, setTurns] = useState(item.turns.join('\n'));
  const [checkpoints, setCheckpoints] = useState(evaluationCaseJSON(item.checkpoints));
  const [inputFiles, setInputFiles] = useState(evaluationCaseJSON(item.input_files));
  const [referenceFiles, setReferenceFiles] = useState(evaluationCaseJSON(item.reference_files));
  const [jsonError, setJsonError] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const filesHint = msg(
    'evaluations.filesJSONHint',
    'JSON array of objects with name and file_id. Leave empty to clear the list.',
  );

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    const parsed = caseUpdateInput({ name, desc, turns, checkpoints, inputFiles, referenceFiles });
    if (!parsed) {
      setJsonError(true);
      return;
    }
    setJsonError(false);
    const saved = await update.mutateAsync({ caseId: item.id, body: parsed });
    onSaved(saved);
  }

  return (
    <form className="grid gap-4 px-4 pb-6 text-sm" onSubmit={(event) => void onSubmit(event)}>
      <Label htmlFor="evaluation-case-name">{msg('evaluations.name', 'Name')}</Label>
      <Input id="evaluation-case-name" value={name} onChange={(event) => setName(event.target.value)} required />
      <Label htmlFor="evaluation-case-desc">{msg('evaluations.descriptionField', 'Description')}</Label>
      <Textarea id="evaluation-case-desc" value={desc} onChange={(event) => setDesc(event.target.value)} />
      <Label htmlFor="evaluation-case-turns">{msg('evaluations.turns', 'Turns')}</Label>
      <Textarea id="evaluation-case-turns" value={turns} onChange={(event) => setTurns(event.target.value)} required />
      <Label htmlFor="evaluation-case-checkpoints">{msg('evaluations.checkpoints', 'Checkpoints')}</Label>
      <Textarea
        id="evaluation-case-checkpoints"
        value={checkpoints}
        onChange={(event) => setCheckpoints(event.target.value)}
      />
      <CaseFileField
        id="evaluation-case-input-files"
        label={msg('evaluations.inputFiles', 'Input files')}
        hint={filesHint}
        value={inputFiles}
        onChange={setInputFiles}
        files={evaluationCaseFiles(item.input_files)}
        workspaceId={workspaceId}
        empty={msg('evaluations.noFiles', 'None')}
        downloadLabel={msg('evaluations.downloadFile', 'Download')}
      />
      <CaseFileField
        id="evaluation-case-reference-files"
        label={msg('evaluations.referenceFiles', 'Reference files')}
        hint={filesHint}
        value={referenceFiles}
        onChange={setReferenceFiles}
        files={evaluationCaseFiles(item.reference_files)}
        workspaceId={workspaceId}
        empty={msg('evaluations.noFiles', 'None')}
        downloadLabel={msg('evaluations.downloadFile', 'Download')}
      />
      {jsonError ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.caseJSONInvalid', 'Checkpoints and file lists must be valid JSON.')}
        </p>
      ) : null}
      {update.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(update.error)}</p> : null}
      {remove.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(remove.error)}</p> : null}
      <div className="flex gap-2">
        <Button type="submit" disabled={update.isPending}>
          {msg('evaluations.saveCase', 'Save')}
        </Button>
        <Button type="button" variant="destructive" onClick={() => setConfirmDelete(true)} disabled={remove.isPending}>
          {msg('evaluations.deleteCase', 'Delete case')}
        </Button>
      </div>
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
                  onDeleted();
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

function CaseFileField({
  id,
  label,
  hint,
  value,
  onChange,
  files,
  workspaceId,
  empty,
  downloadLabel,
}: {
  id: string;
  label: string;
  hint: string;
  value: string;
  onChange: (value: string) => void;
  files: EvaluationCaseFile[];
  workspaceId: string;
  empty: string;
  downloadLabel: string;
}) {
  return (
    <section className="grid gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Textarea id={id} value={value} onChange={(event) => onChange(event.target.value)} />
      <p className="text-muted-foreground">{hint}</p>
      {files.length === 0 ? <p className="text-muted-foreground">{empty}</p> : null}
      {files.map((file) => (
        <div key={`${file.fileId}:${file.name}`} className="flex items-center justify-between gap-3">
          <span className="min-w-0 truncate">{file.name}</span>
          {file.fileId ? (
            <Button
              type="button"
              variant="outline"
              className="h-8 shrink-0"
              onClick={() => void downloadApiFile({ id: file.fileId, filename: file.name }, workspaceId)}
            >
              {downloadLabel}
            </Button>
          ) : null}
        </div>
      ))}
    </section>
  );
}

function caseUpdateInput(draft: {
  name: string;
  desc: string;
  turns: string;
  checkpoints: string;
  inputFiles: string;
  referenceFiles: string;
}): UpdateEvaluationCaseInput | null {
  try {
    return {
      name: draft.name,
      desc: draft.desc,
      turns: draft.turns
        .split('\n')
        .map((turn) => turn.trim())
        .filter((turn) => turn.length > 0),
      checkpoints: parseCaseJSON(draft.checkpoints, false),
      input_files: parseCaseJSON(draft.inputFiles, true),
      reference_files: parseCaseJSON(draft.referenceFiles, true),
    };
  } catch {
    return null;
  }
}

function parseCaseJSON(value: string, arrayOnly: boolean) {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  const parsed: unknown = JSON.parse(trimmed);
  if (arrayOnly) {
    if (!Array.isArray(parsed)) {
      throw new Error('array');
    }
    return parsed;
  }
  if (parsed === null || typeof parsed !== 'object') {
    throw new Error('json');
  }
  return parsed;
}
