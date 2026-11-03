import { Download } from 'lucide-react';

import { downloadApiFile } from '../../shared/api/file-download';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { type EvaluationCase } from './api';
import { evaluationCaseFiles, evaluationCaseJSON } from './caseFiles';

export function CaseDetailSheet({
  item,
  workspaceId,
  open,
  onOpenChange,
}: {
  item: EvaluationCase | null;
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { msg } = useI18n();
  const files = evaluationCaseFiles(item?.input_files);
  const checkpoints = stringList(item?.checkpoints);
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{msg('evaluations.caseDetail', 'Case')}</SheetTitle>
        </SheetHeader>
        {item ? (
          <div className="grid gap-6 px-4 pb-6">
            <section className="grid gap-3 rounded-lg border p-4">
              <h2 className="text-sm font-medium">{msg('evaluations.caseContent', 'Case')}</h2>
              <DetailRow label={msg('evaluations.name', 'Name')} value={item.name} />
              <DetailRow label={msg('evaluations.caseId', 'Case ID')} value={item.id} mono />
              <div className="grid gap-1">
                <p className="text-sm text-muted-foreground">{msg('evaluations.caseInput', 'Input')}</p>
                <div className="rounded-md bg-muted px-3 py-2 text-sm whitespace-pre-wrap">
                  {item.turns.map((turn, index) => (
                    <p key={`${index}-${turn}`}>
                      {item.turns.length > 1 ? `${index + 1}. ` : ''}
                      {turn}
                    </p>
                  ))}
                </div>
              </div>
              {checkpoints.length > 0 ? (
                <div className="grid gap-1">
                  <p className="text-sm text-muted-foreground">{msg('evaluations.checkpoints', 'Checkpoints')}</p>
                  <ul className="list-disc space-y-1 pl-5 text-sm">
                    {checkpoints.map((line) => (
                      <li key={line}>{line}</li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </section>
            <section className="grid gap-3 rounded-lg border p-4">
              <h2 className="text-sm font-medium">{msg('evaluations.inputFiles', 'Input files')}</h2>
              {files.length === 0 ? (
                <p className="text-sm text-muted-foreground">{msg('evaluations.noFiles', 'None')}</p>
              ) : (
                files.map((file) => (
                  <div key={`${file.fileId}:${file.name}`} className="flex items-center justify-between gap-3 text-sm">
                    <span className="min-w-0 truncate">{file.name}</span>
                    {file.fileId ? (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="text-muted-foreground"
                        onClick={() => void downloadApiFile({ id: file.fileId, filename: file.name }, workspaceId)}
                      >
                        <Download className="size-4" aria-hidden />
                        {msg('evaluations.downloadFile', 'Download')}
                      </Button>
                    ) : null}
                  </div>
                ))
              )}
            </section>
            <section className="grid gap-3 rounded-lg border p-4">
              <h2 className="text-sm font-medium">{msg('evaluations.fullCase', 'Full case')}</h2>
              <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-5 whitespace-pre-wrap">
                {evaluationCaseJSON(caseSnapshot(item))}
              </pre>
            </section>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function DetailRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[5.5rem_1fr] gap-3 text-sm">
      <p className="text-muted-foreground">{label}</p>
      <p className={mono ? 'font-mono text-xs break-all' : 'break-all'}>{value}</p>
    </div>
  );
}

function stringList(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is string => typeof item === 'string' && item.length > 0);
}

function caseSnapshot(item: EvaluationCase) {
  return {
    name: item.name,
    turns: item.turns,
    desc: item.desc,
    checkpoints: item.checkpoints,
    input_files: item.input_files,
    reference_files: item.reference_files,
  };
}
