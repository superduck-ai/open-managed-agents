import { useI18n } from '../../shared/i18n';
import { downloadApiFile } from '../../shared/api/file-download';
import { Button } from '../../shared/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { evaluationCaseFiles, evaluationCaseJSON, type EvaluationCaseFile } from './caseFiles';
import type { EvaluationCase } from './api';

export function CaseDrawer({
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
  const inputFiles = evaluationCaseFiles(item?.input_files);
  const referenceFiles = evaluationCaseFiles(item?.reference_files);
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="overflow-y-auto">
        <SheetHeader>
          <SheetTitle>{item?.name ?? msg('evaluations.caseDetail', 'Case')}</SheetTitle>
          <SheetDescription>
            {msg(
              'evaluations.caseReadOnly',
              'Cases can be added or imported. Editing or deleting a case is not available.',
            )}
          </SheetDescription>
        </SheetHeader>
        {item ? (
          <div className="grid gap-4 px-4 pb-6 text-sm">
            {item.desc ? <p className="text-muted-foreground">{item.desc}</p> : null}
            <CaseBlock title={msg('evaluations.turns', 'Turns')} body={item.turns.join('\n')} />
            <CaseBlock
              title={msg('evaluations.checkpoints', 'Checkpoints')}
              body={evaluationCaseJSON(item.checkpoints)}
            />
            <CaseFiles
              title={msg('evaluations.inputFiles', 'Input files')}
              files={inputFiles}
              workspaceId={workspaceId}
              empty={msg('evaluations.noFiles', 'None')}
              downloadLabel={msg('evaluations.downloadFile', 'Download')}
            />
            <CaseFiles
              title={msg('evaluations.referenceFiles', 'Reference files')}
              files={referenceFiles}
              workspaceId={workspaceId}
              empty={msg('evaluations.noFiles', 'None')}
              downloadLabel={msg('evaluations.downloadFile', 'Download')}
            />
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function CaseBlock({ title, body }: { title: string; body: string }) {
  return (
    <section className="grid gap-1">
      <h2 className="font-medium">{title}</h2>
      <pre className="whitespace-pre-wrap text-muted-foreground">{body || '—'}</pre>
    </section>
  );
}

function CaseFiles({
  title,
  files,
  workspaceId,
  empty,
  downloadLabel,
}: {
  title: string;
  files: EvaluationCaseFile[];
  workspaceId: string;
  empty: string;
  downloadLabel: string;
}) {
  return (
    <section className="grid gap-2">
      <h2 className="font-medium">{title}</h2>
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
