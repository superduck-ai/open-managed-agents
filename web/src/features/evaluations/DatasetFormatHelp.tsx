import { Copy } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogTitle } from '../../shared/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { datasetZipTree } from './datasetTemplate';

export function DatasetFormatHelp({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { msg } = useI18n();
  const rows = caseFieldRows(msg);
  const markdown = helpMarkdown(msg, rows);
  const [copied, setCopied] = useState(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <div className="flex items-center justify-between gap-3 pr-8">
          <DialogTitle>{msg('evaluations.createHelp', 'How to create a dataset')}</DialogTitle>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              const write = navigator.clipboard?.writeText(markdown);
              if (!write) {
                return;
              }
              void write.then(() => {
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1200);
              });
            }}
          >
            <Copy className="size-4" aria-hidden />
            {copied ? msg('evaluations.copied', 'Copied') : msg('evaluations.copyMarkdown', 'Copy Markdown')}
          </Button>
        </div>
        <Tabs defaultValue="reading">
          <TabsList variant="line">
            <TabsTrigger value="reading">{msg('evaluations.helpRead', 'Reading')}</TabsTrigger>
            <TabsTrigger value="markdown">{msg('evaluations.helpSource', 'Markdown')}</TabsTrigger>
          </TabsList>
          <TabsContent value="reading" className="max-h-[min(36rem,calc(100vh-10rem))] overflow-y-auto">
            <HelpReading rows={rows} />
          </TabsContent>
          <TabsContent value="markdown">
            <pre className="max-h-[min(36rem,calc(100vh-10rem))] overflow-auto rounded-lg bg-muted p-4 font-mono text-xs leading-5 whitespace-pre-wrap">
              {markdown}
            </pre>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function HelpReading({ rows }: { rows: CaseFieldRow[] }) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-6 py-2">
      <section className="grid gap-3">
        <h2 className="text-lg font-semibold">{msg('evaluations.helpFormatTitle', 'File format')}</h2>
        <p className="text-sm leading-6">
          <RichText
            text={msg(
              'evaluations.helpFormatBody',
              'A dataset is a list of cases. You can upload a .jsonl or a .zip file.',
            )}
          />
        </p>
        <ul className="list-disc space-y-2 pl-5 text-sm leading-6">
          <li>
            <RichText
              text={msg(
                'evaluations.helpJsonl',
                'A JSONL file has one case per line and does not include attachments.',
              )}
            />
          </li>
          <li>
            <RichText
              text={msg(
                'evaluations.helpZip',
                'A ZIP must include cases.jsonl. It may also include input_files/ and reference_files/. Paths in the case are relative to those folders.',
              )}
            />
          </li>
        </ul>
        <pre className="overflow-x-auto rounded-lg bg-foreground px-4 py-3 font-mono text-xs leading-5 text-background">
          {datasetZipTree}
        </pre>
        <p className="text-sm leading-6">
          <RichText
            text={msg(
              'evaluations.helpPaths',
              '`input_files` and `reference_files` store paths relative to those folders. A file at `input_files/sales/2025/Q1/orders.xlsx` is written as `"sales/2025/Q1/orders.xlsx"`.',
            )}
          />
        </p>
      </section>
      <section className="grid gap-3">
        <h2 className="text-lg font-semibold">{msg('evaluations.helpFieldsTitle', 'Case fields')}</h2>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{msg('evaluations.helpField', 'Field')}</TableHead>
              <TableHead>{msg('evaluations.helpRequired', 'Required')}</TableHead>
              <TableHead>{msg('evaluations.helpType', 'Type')}</TableHead>
              <TableHead>{msg('evaluations.helpMeaning', 'What it means')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.field}>
                <TableCell className="font-mono text-xs">{row.field}</TableCell>
                <TableCell>
                  {row.required ? msg('evaluations.helpYes', 'Yes') : msg('evaluations.helpNo', 'No')}
                </TableCell>
                <TableCell className="font-mono text-xs">{row.type}</TableCell>
                <TableCell className="text-muted-foreground">{row.meaning}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </section>
    </div>
  );
}

function RichText({ text }: { text: string }) {
  return text.split(/(`[^`]+`)/).map((part, index) =>
    part.startsWith('`') && part.endsWith('`') ? (
      <code key={index} className="rounded bg-muted px-1 font-mono text-[0.85em]">
        {part.slice(1, -1)}
      </code>
    ) : (
      <span key={index}>{part}</span>
    ),
  );
}

type CaseFieldRow = { field: string; required: boolean; type: string; meaning: string };

function caseFieldRows(msg: ReturnType<typeof useI18n>['msg']): CaseFieldRow[] {
  return [
    {
      field: 'name',
      required: true,
      type: 'string',
      meaning: msg('evaluations.helpName', 'Required. A name for the case.'),
    },
    {
      field: 'turns',
      required: true,
      type: 'string[]',
      meaning: msg(
        'evaluations.helpTurns',
        'Required. At least one non-empty string. These turns are sent to the agent under test.',
      ),
    },
    {
      field: 'desc',
      required: false,
      type: 'string',
      meaning: msg('evaluations.helpDesc', 'Optional. A note for you.'),
    },
    {
      field: 'checkpoints',
      required: false,
      type: 'string[]',
      meaning: msg(
        'evaluations.helpCheckpoints',
        'Optional. Acceptance checks the evaluator compares against. Not instructions sent to the agent under test.',
      ),
    },
    {
      field: 'input_files',
      required: false,
      type: 'string[]',
      meaning: msg(
        'evaluations.helpInputFiles',
        'Optional. Relative paths. These files are given to the agent under test.',
      ),
    },
    {
      field: 'reference_files',
      required: false,
      type: 'string[]',
      meaning: msg(
        'evaluations.helpReferenceFiles',
        'Optional. Relative paths. Reference answers for the evaluator only. They are not given to the agent under test.',
      ),
    },
  ];
}

function helpMarkdown(msg: ReturnType<typeof useI18n>['msg'], rows: CaseFieldRow[]) {
  const yes = msg('evaluations.helpYes', 'Yes');
  const no = msg('evaluations.helpNo', 'No');
  const table = [
    `| ${msg('evaluations.helpField', 'Field')} | ${msg('evaluations.helpRequired', 'Required')} | ${msg('evaluations.helpType', 'Type')} | ${msg('evaluations.helpMeaning', 'What it means')} |`,
    '| --- | --- | --- | --- |',
    ...rows.map((row) => `| \`${row.field}\` | ${row.required ? yes : no} | ${row.type} | ${row.meaning} |`),
  ];
  return [
    `## ${msg('evaluations.helpFormatTitle', 'File format')}`,
    '',
    msg('evaluations.helpFormatBody', 'A dataset is a list of cases. You can upload a .jsonl or a .zip file.'),
    '',
    `- ${msg('evaluations.helpJsonl', 'A JSONL file has one case per line and does not include attachments.')}`,
    `- ${msg('evaluations.helpZip', 'A ZIP must include cases.jsonl. It may also include input_files/ and reference_files/. Paths in the case are relative to those folders.')}`,
    '',
    '```',
    datasetZipTree,
    '```',
    '',
    msg(
      'evaluations.helpPaths',
      '`input_files` and `reference_files` store paths relative to those folders. A file at `input_files/sales/2025/Q1/orders.xlsx` is written as `"sales/2025/Q1/orders.xlsx"`.',
    ),
    '',
    `## ${msg('evaluations.helpFieldsTitle', 'Case fields')}`,
    '',
    ...table,
  ].join('\n');
}
