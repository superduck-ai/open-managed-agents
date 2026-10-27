import { Search } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Input } from '../../shared/ui/input';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import {
  evaluationErrorMessage,
  type EvaluationCase,
  type EvaluationDatasetPreset,
  type EvaluationPresetVersion,
} from './api';
import { CaseDetailSheet } from './CaseDrawer';
import { CaseListPager, casePageSlice } from './CaseListPager';
import { CopyIdButton } from './CopyIdButton';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import {
  datasetIdFromPath,
  evaluationsIndexHref,
  useEvaluationDatasetPreset,
  useEvaluationDatasetPresetCases,
} from './useEvaluationDatasets';

export function PresetDatasetPage() {
  const { msg } = useI18n();
  const presetId = datasetIdFromPath();
  const { workspace, query } = useEvaluationDatasetPreset(presetId);
  const preset = query.data;
  const latest = latestVersion(preset);
  const [versionId, setVersionId] = useState('');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);
  const [openCase, setOpenCase] = useState<EvaluationCase | null>(null);
  const selectedId = versionId || latest?.id || '';
  const selected = selectedPresetVersion(preset, selectedId, latest);
  const casesQuery = useEvaluationDatasetPresetCases(presetId, selectedId);
  const cases = visiblePresetCases(casesQuery.data?.data ?? [], search);
  const visible = casePageSlice(cases, page);

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="datasets" />
      <header className="mb-3">
        <a
          className="text-sm text-muted-foreground hover:text-foreground"
          href={evaluationsIndexHref(workspace.workspaceId)}
        >
          {msg('evaluations.title', 'Datasets')}
        </a>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
          {preset?.name ?? msg('evaluations.cases', 'Cases')}
        </h1>
      </header>
      {preset ? (
        <div className="mb-4 flex items-center gap-2">
          <span className="inline-flex h-5 shrink-0 items-center rounded-md bg-muted px-1.5 text-xs font-medium text-muted-foreground">
            {msg('evaluations.builtinBadge', 'Built-in')}
          </span>
          <CopyIdButton id={preset.id} className="w-auto" />
        </div>
      ) : null}
      <div className="flex flex-col gap-8">
        <section className="rounded-lg border px-5 py-5">
          <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-baseline gap-x-8 gap-y-4 text-sm">
            <dt className="text-muted-foreground">{msg('evaluations.descriptionField', 'Description')}</dt>
            <dd className="min-w-0 truncate">{preset?.desc || '—'}</dd>
            <dt className="text-muted-foreground">{msg('evaluations.caseCount', 'Cases')}</dt>
            <dd>{selected?.case_count ?? 0}</dd>
            <dt className="text-muted-foreground">{msg('evaluations.createdAt', 'Created')}</dt>
            <dd>{dateLabel(preset?.published_at ?? '')}</dd>
          </dl>
        </section>
        <PresetVersions
          preset={preset}
          loading={query.isLoading}
          errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
          onRetry={() => void query.refetch()}
          onView={setVersionId}
        />
        <section id="evaluation-cases" className="rounded-lg border">
          <div className="px-4 pt-4">
            <h2 className="text-sm font-medium">{msg('evaluations.caseList', 'Cases')}</h2>
          </div>
          <div className="flex flex-wrap items-center gap-3 px-4 py-3">
            <p className="text-sm">
              <span className="text-muted-foreground">{msg('evaluations.currentVersion', 'Current version')}: </span>
              {selected ? `v${selected.version}` : ''}
            </p>
            {preset ? <EvaluationStatusBadge status={preset.status} /> : null}
          </div>
          <div className="px-4">
            <div className="relative mb-3 max-w-sm">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value);
                  setPage(0);
                }}
                placeholder={msg('evaluations.searchDatasetCases', 'Search cases')}
                aria-label={msg('evaluations.searchDatasetCases', 'Search cases')}
                className="h-9 bg-background pl-8"
              />
            </div>
          </div>
          <div className="max-h-80 overflow-auto">
          <Table className="table-fixed [&_td]:px-4 [&_td]:py-3 [&_th]:sticky [&_th]:top-0 [&_th]:z-10 [&_th]:bg-muted [&_th]:h-9 [&_th]:px-4">
            <TableHeader>
              <TableRow className="bg-muted hover:bg-muted">
                <TableHead className="w-40 text-xs font-normal text-muted-foreground">
                  {msg('evaluations.caseTitle', 'Name')}
                </TableHead>
                <TableHead className="text-xs font-normal text-muted-foreground">
                  {msg('evaluations.caseInput', 'Input')}
                </TableHead>
                <TableHead className="w-24 text-xs font-normal text-muted-foreground">
                  {msg('evaluations.caseMode', 'Mode')}
                </TableHead>
                <TableHead className="w-24 text-right text-xs font-normal text-muted-foreground">
                  {msg('evaluations.actions', 'Action')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <PresetCaseRows
                cases={visible.items}
                isLoading={casesQuery.isLoading}
                errorMessage={casesQuery.error ? evaluationErrorMessage(casesQuery.error) : ''}
                onRetry={() => void casesQuery.refetch()}
                onOpen={setOpenCase}
              />
            </TableBody>
          </Table>
          </div>
          <CaseListPager total={cases.length} page={visible.page} pageCount={visible.pageCount} onPage={setPage} />
        </section>
      </div>
      <CaseDetailSheet
        item={openCase}
        workspaceId={workspace.workspaceId}
        open={openCase !== null}
        onOpenChange={(open) => {
          if (!open) {
            setOpenCase(null);
          }
        }}
      />
    </section>
  );
}

function PresetVersions({
  preset,
  loading,
  errorMessage,
  onRetry,
  onView,
}: {
  preset: EvaluationDatasetPreset | undefined;
  loading: boolean;
  errorMessage: string;
  onRetry: () => void;
  onView: (versionId: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <section className="rounded-lg border">
      <h2 className="px-4 pt-4 pb-3 text-sm font-medium">{msg('evaluations.versionList', 'Versions')}</h2>
      <Table className="table-fixed [&_td]:px-4 [&_th]:h-9 [&_th]:px-4">
        <TableHeader>
          <TableRow className="bg-muted hover:bg-muted">
            <TableHead className="w-16 text-xs font-normal text-muted-foreground">
              {msg('evaluations.versionColumn', 'Version')}
            </TableHead>
            <TableHead className="text-xs font-normal text-muted-foreground">{msg('evaluations.id', 'ID')}</TableHead>
            <TableHead className="w-28 text-xs font-normal text-muted-foreground">
              {msg('evaluations.status', 'Status')}
            </TableHead>
            <TableHead className="w-24 text-xs font-normal text-muted-foreground">
              {msg('evaluations.caseCount', 'Cases')}
            </TableHead>
            <TableHead className="w-32 text-xs font-normal text-muted-foreground">
              {msg('evaluations.publishedAt', 'Published')}
            </TableHead>
            <TableHead className="w-28 text-right text-xs font-normal text-muted-foreground">
              {msg('evaluations.actions', 'Action')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <PresetVersionRows
            preset={preset}
            loading={loading}
            errorMessage={errorMessage}
            onRetry={onRetry}
            onView={onView}
          />
        </TableBody>
      </Table>
    </section>
  );
}

function PresetVersionRows({
  preset,
  loading,
  errorMessage,
  onRetry,
  onView,
}: {
  preset: EvaluationDatasetPreset | undefined;
  loading: boolean;
  errorMessage: string;
  onRetry: () => void;
  onView: (versionId: string) => void;
}) {
  const { msg } = useI18n();
  if (loading) {
    return <TableLoadingRow colSpan={6} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={6}
        title={msg('evaluations.presetsLoadFailed', 'Could not load presets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (!preset || preset.versions.length === 0) {
    return <TableEmptyRow colSpan={6}>{msg('common.loading', 'Loading...')}</TableEmptyRow>;
  }
  return preset.versions.map((version) => (
    <PresetVersionRow key={version.id} preset={preset} version={version} onView={onView} />
  ));
}

function PresetVersionRow({
  preset,
  version,
  onView,
}: {
  preset: EvaluationDatasetPreset;
  version: EvaluationPresetVersion;
  onView: (versionId: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <TableRow>
      <TableCell>v{version.version}</TableCell>
      <TableCell>
        <CopyIdButton id={version.id} className="max-w-48" />
      </TableCell>
      <TableCell>
        <EvaluationStatusBadge status={preset.status} />
      </TableCell>
      <TableCell>{version.case_count}</TableCell>
      <TableCell className="text-muted-foreground">{dateLabel(preset.published_at)}</TableCell>
      <TableCell className="text-right">
        <a
          className="text-sm text-muted-foreground hover:text-foreground"
          href="#evaluation-cases"
          onClick={() => onView(version.id)}
        >
          {msg('evaluations.viewCase', 'View')}
        </a>
      </TableCell>
    </TableRow>
  );
}

function PresetCaseRows({
  cases,
  isLoading,
  errorMessage,
  onRetry,
  onOpen,
}: {
  cases: EvaluationCase[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
  onOpen: (item: EvaluationCase) => void;
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
      <TableCell className="w-40 font-medium">
        <span className="block truncate">{item.name}</span>
      </TableCell>
      <TableCell className="text-muted-foreground">
        <span className="block truncate">{item.turns[0] ?? ''}</span>
      </TableCell>
      <TableCell className="text-muted-foreground">
        {item.turns.length > 1
          ? msg('evaluations.multiTurn', 'Multi-turn')
          : msg('evaluations.singleTurn', 'Single turn')}
      </TableCell>
      <TableCell className="text-right">
        <button
          type="button"
          className="text-sm text-muted-foreground hover:text-foreground"
          aria-label={`${msg('evaluations.viewCase', 'View')} ${item.name}`}
          onClick={() => onOpen(item)}
        >
          {msg('evaluations.viewCase', 'View')}
        </button>
      </TableCell>
    </TableRow>
  ));
}

function selectedPresetVersion(
  preset: EvaluationDatasetPreset | undefined,
  selectedId: string,
  latest: EvaluationPresetVersion | undefined,
) {
  return preset?.versions.find((version) => version.id === selectedId) ?? latest;
}

function visiblePresetCases(cases: EvaluationCase[], search: string) {
  return cases.filter((item) => matchesCaseQuery(search, [item.name, item.desc, ...item.turns]));
}

function latestVersion(preset: EvaluationDatasetPreset | undefined) {
  return preset?.versions.reduce<EvaluationPresetVersion | undefined>(
    (current, version) => (!current || version.version > current.version ? version : current),
    undefined,
  );
}

function matchesCaseQuery(query: string, parts: string[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.some((part) => part.toLowerCase().includes(needle));
}

function dateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}
