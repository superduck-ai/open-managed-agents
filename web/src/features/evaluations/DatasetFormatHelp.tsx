import { useI18n, useLocale } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { datasetZipTree, downloadDatasetTemplate } from './datasetTemplate';

export function DatasetFormatHelp({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const { locale } = useLocale();
  const rows = caseFieldRows(msg);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{msg('evaluations.createHelp', 'How to create a dataset')}</DialogTitle>
        </DialogHeader>
        <div className="grid max-h-[min(36rem,calc(100vh-8rem))] gap-4 overflow-y-auto pr-1">
          <section className="grid gap-2">
            <h2 className="text-sm font-medium">{msg('evaluations.helpFormatTitle', 'File format')}</h2>
            <p className="text-sm text-muted-foreground">
              {msg(
                'evaluations.helpFormatBody',
                'A dataset is a list of cases. You can upload a .jsonl or a .zip file.',
              )}
            </p>
            <p className="text-sm text-muted-foreground">
              {msg('evaluations.helpJsonl', 'A JSONL file has one case per line and does not include attachments.')}
            </p>
            <p className="text-sm text-muted-foreground">
              {msg(
                'evaluations.helpZip',
                'A ZIP must include cases.jsonl. It may also include input_files/ and reference_files/. Paths in the case are relative to those folders.',
              )}
            </p>
            <p className="text-sm font-medium">{msg('evaluations.helpTreeLabel', 'For example')}</p>
            <pre className="overflow-x-auto rounded-lg bg-muted px-3 py-2 font-mono text-xs leading-5">
              {datasetZipTree}
            </pre>
          </section>
          <section className="grid gap-2">
            <h2 className="text-sm font-medium">{msg('evaluations.helpFieldsTitle', 'Case fields')}</h2>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{msg('evaluations.helpField', 'Field')}</TableHead>
                  <TableHead>{msg('evaluations.helpType', 'Type')}</TableHead>
                  <TableHead>{msg('evaluations.helpMeaning', 'What it means')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row) => (
                  <TableRow key={row.field}>
                    <TableCell className="font-mono text-xs">{row.field}</TableCell>
                    <TableCell className="font-mono text-xs">{row.type}</TableCell>
                    <TableCell className="text-muted-foreground">{row.meaning}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className="text-sm text-muted-foreground">
              {msg(
                'evaluations.helpCheckpointsNote',
                'On the case form, write one checkpoint per line. Saving stores them as a string array.',
              )}
            </p>
          </section>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => downloadDatasetTemplate(locale)}>
            {msg('evaluations.downloadTemplate', 'Download template')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function caseFieldRows(msg: ReturnType<typeof useI18n>['msg']) {
  return [
    { field: 'name', type: 'string', meaning: msg('evaluations.helpName', 'Required. A name for the case.') },
    {
      field: 'turns',
      type: 'string[]',
      meaning: msg(
        'evaluations.helpTurns',
        'Required. At least one non-empty string. These turns are sent to the agent under test.',
      ),
    },
    { field: 'desc', type: 'string', meaning: msg('evaluations.helpDesc', 'Optional. A note for you.') },
    {
      field: 'checkpoints',
      type: 'string[]',
      meaning: msg(
        'evaluations.helpCheckpoints',
        'Optional. Acceptance checks the evaluator compares against. Not instructions sent to the agent under test.',
      ),
    },
    {
      field: 'input_files',
      type: 'string[]',
      meaning: msg(
        'evaluations.helpInputFiles',
        'Optional. Relative paths. These files are given to the agent under test.',
      ),
    },
    {
      field: 'reference_files',
      type: 'string[]',
      meaning: msg(
        'evaluations.helpReferenceFiles',
        'Optional. Relative paths. Reference answers for the evaluator only. They are not given to the agent under test.',
      ),
    },
  ];
}
