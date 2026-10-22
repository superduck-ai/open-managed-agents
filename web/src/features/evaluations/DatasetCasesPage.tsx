import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationErrorMessage, type EvaluationCase } from './api';
import {
  datasetIdFromPath,
  evaluationsIndexHref,
  useCreateEvaluationCase,
  useEvaluationCaseList,
  useImportEvaluationCases,
} from './useEvaluationDatasets';

export function DatasetCasesPage() {
  const { msg } = useI18n();
  const datasetId = datasetIdFromPath();
  const { workspace, datasetQuery, casesQuery } = useEvaluationCaseList(datasetId);
  const cases = casesQuery.data?.data ?? [];
  const version = datasetQuery.data?.version;

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5">
        <a
          className="text-sm text-muted-foreground underline-offset-4 hover:underline"
          href={evaluationsIndexHref(workspace.workspaceId)}
        >
          {msg('evaluations.title', 'Datasets')}
        </a>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
          {datasetQuery.data?.name ?? msg('evaluations.cases', 'Cases')}
        </h1>
        {version ? (
          <p className="mt-1 text-sm text-muted-foreground">
            {msg('evaluations.version', 'Version {version}', { version: version.version })}
          </p>
        ) : null}
        {datasetQuery.data?.desc ? (
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">{datasetQuery.data.desc}</p>
        ) : null}
      </header>
      <div className="mb-8 grid gap-6 lg:grid-cols-2">
        <AddCaseForm datasetId={datasetId} />
        <ImportCasesForm datasetId={datasetId} />
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.position', 'Position')}</TableHead>
            <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
            <TableHead>{msg('evaluations.turns', 'Turns')}</TableHead>
            <TableHead>{msg('evaluations.descriptionField', 'Description')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <CaseRows
            cases={cases}
            isLoading={!workspace.workspaceReady || casesQuery.isLoading}
            errorMessage={casesQuery.error ? evaluationErrorMessage(casesQuery.error) : ''}
            onRetry={() => void casesQuery.refetch()}
          />
        </TableBody>
      </Table>
    </section>
  );
}

function CaseRows({
  cases,
  isLoading,
  errorMessage,
  onRetry,
}: {
  cases: EvaluationCase[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={4} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={4}
        title={msg('evaluations.casesLoadFailed', 'Could not load cases')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (cases.length === 0) {
    return <TableEmptyRow colSpan={4}>{msg('evaluations.noCases', 'No cases yet.')}</TableEmptyRow>;
  }
  return cases.map((item) => (
    <TableRow key={item.id}>
      <TableCell>{item.position}</TableCell>
      <TableCell>{item.name}</TableCell>
      <TableCell className="whitespace-pre-wrap">{item.turns.join('\n')}</TableCell>
      <TableCell>{item.desc}</TableCell>
    </TableRow>
  ));
}

function AddCaseForm({ datasetId }: { datasetId: string }) {
  const { msg } = useI18n();
  const create = useCreateEvaluationCase(datasetId);
  const [name, setName] = useState('');
  const [turns, setTurns] = useState('');
  const [desc, setDesc] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      {
        name,
        desc,
        turns: turns
          .split('\n')
          .map((turn) => turn.trim())
          .filter((turn) => turn.length > 0),
      },
      {
        onSuccess: () => {
          setName('');
          setTurns('');
          setDesc('');
        },
      },
    );
  }

  return (
    <form className="grid gap-3" onSubmit={submit}>
      <h2 className="text-sm font-medium">{msg('evaluations.addCase', 'Add case')}</h2>
      <Label htmlFor="evaluation-case-name">{msg('evaluations.name', 'Name')}</Label>
      <Input id="evaluation-case-name" value={name} onChange={(event) => setName(event.target.value)} required />
      <Label htmlFor="evaluation-case-turns">{msg('evaluations.turns', 'Turns')}</Label>
      <Textarea id="evaluation-case-turns" value={turns} onChange={(event) => setTurns(event.target.value)} required />
      <Label htmlFor="evaluation-case-desc">{msg('evaluations.descriptionField', 'Description')}</Label>
      <Textarea id="evaluation-case-desc" value={desc} onChange={(event) => setDesc(event.target.value)} />
      {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
      <Button type="submit" disabled={create.isPending}>
        {msg('evaluations.addCase', 'Add case')}
      </Button>
    </form>
  );
}

function ImportCasesForm({ datasetId }: { datasetId: string }) {
  const { msg } = useI18n();
  const importCases = useImportEvaluationCases(datasetId);
  const [jsonl, setJsonl] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    importCases.mutate(jsonl, { onSuccess: () => setJsonl('') });
  }

  return (
    <form className="grid gap-3" onSubmit={submit}>
      <h2 className="text-sm font-medium">{msg('evaluations.importJsonl', 'Import JSONL')}</h2>
      <Label htmlFor="evaluation-case-jsonl">
        {msg('evaluations.importHint', 'One JSON object per line, with name and turns.')}
      </Label>
      <Textarea id="evaluation-case-jsonl" value={jsonl} onChange={(event) => setJsonl(event.target.value)} required />
      {importCases.error ? (
        <p className="text-sm text-destructive">{evaluationErrorMessage(importCases.error)}</p>
      ) : null}
      <Button type="submit" disabled={importCases.isPending}>
        {msg('evaluations.import', 'Import')}
      </Button>
    </form>
  );
}
