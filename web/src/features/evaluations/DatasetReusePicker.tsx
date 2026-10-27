import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { evaluationErrorMessage, type EvaluationCase } from './api';
import {
  reuseOptions,
  selectedReuseOption,
  selectedReuseVersion,
  setCaseSelected,
  setCasesSelected,
  type ReuseOption,
} from './reuseSources';
import {
  useEvaluationDatasetList,
  useEvaluationDatasetPresetList,
  useEvaluationReuseCases,
} from './useEvaluationDatasets';

const checkedCaseClass = 'data-checked:border-[#267D30]! data-checked:bg-[#267D30]! data-checked:text-white!';

export function useDatasetReuse(active: boolean) {
  const datasets = useEvaluationDatasetList();
  const presets = useEvaluationDatasetPresetList(active);
  const [sourceKey, setSourceKey] = useState('');
  const [versionId, setVersionId] = useState('');
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const options = reuseOptions(datasets.query.data?.data ?? [], presets.query.data?.data ?? []);
  const option = selectedReuseOption(options, sourceKey);
  const version = selectedReuseVersion(option, versionId);
  const casesQuery = useEvaluationReuseCases(active ? (option?.kind ?? '') : '', option?.id ?? '', version?.id ?? '');
  const cases = casesQuery.data ?? [];
  return {
    options,
    option,
    version,
    cases,
    selected: selectedOrder(cases, selectedIds),
    casesQuery,
    loadingLists: (datasets.query.isLoading || presets.query.isLoading) && options.length === 0,
    chooseSource(key: string) {
      setSourceKey(key);
      setVersionId('');
      setSelectedIds([]);
    },
    chooseVersion(id: string) {
      setVersionId(id);
      setSelectedIds([]);
    },
    setSelected(id: string, selected: boolean) {
      setSelectedIds((current) => setCaseSelected(current, id, selected));
    },
    setMany(ids: string[], selected: boolean) {
      setSelectedIds((current) => setCasesSelected(current, ids, selected));
    },
    clearSelected() {
      setSelectedIds([]);
    },
    reset() {
      setSourceKey('');
      setVersionId('');
      setSelectedIds([]);
    },
  };
}

export function DatasetReusePicker({ reuse }: { reuse: ReturnType<typeof useDatasetReuse> }) {
  const { msg } = useI18n();
  const [query, setQuery] = useState('');
  const [detail, setDetail] = useState<EvaluationCase | null>(null);
  if (reuse.loadingLists) {
    return <p className="text-sm text-muted-foreground">{msg('evaluations.reuseLoading', 'Loading cases…')}</p>;
  }
  if (reuse.options.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {msg('evaluations.noDatasetsToReuse', 'No datasets to reuse yet.')}
      </p>
    );
  }
  if (!reuse.option || !reuse.version) {
    return null;
  }
  const filtered = reuse.cases.filter((item) => matchesCase(query, item));
  return (
    <div className="grid gap-3">
      <div className="grid gap-1">
        <div className="flex items-center gap-0.5">
          <span className="text-destructive" aria-hidden>
            *
          </span>
          <p className="text-sm font-medium">{msg('evaluations.reusePickLabel', 'Select cases from the left panel')}</p>
        </div>
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.reuseHint', 'Pick cases from an existing dataset and combine them into a new one.')}
        </p>
      </div>
      <div className="grid items-start gap-3 md:grid-cols-2">
        <section className="grid gap-3 rounded-lg border p-3">
          <h3 className="text-sm font-medium">{msg('evaluations.reusePanelTitle', 'Dataset and cases')}</h3>
          <ReuseSelects
            options={reuse.options}
            option={reuse.option}
            versionId={reuse.version.id}
            onSource={reuse.chooseSource}
            onVersion={reuse.chooseVersion}
          />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={msg('evaluations.searchReuseCases', 'Search cases...')}
            aria-label={msg('evaluations.searchReuseCases', 'Search cases...')}
          />
          <ReuseCaseBody reuse={reuse} filtered={filtered} onDetail={setDetail} />
        </section>
        <SelectedCases reuse={reuse} />
      </div>
      <CasePreview
        item={detail}
        source={reuse.option.name}
        version={reuse.version.version}
        onClose={() => setDetail(null)}
      />
    </div>
  );
}

function ReuseCaseBody({
  reuse,
  filtered,
  onDetail,
}: {
  reuse: ReturnType<typeof useDatasetReuse>;
  filtered: EvaluationCase[];
  onDetail: (item: EvaluationCase) => void;
}) {
  const { msg } = useI18n();
  if (reuse.casesQuery.isLoading) {
    return <p className="text-sm text-muted-foreground">{msg('evaluations.reuseLoading', 'Loading cases…')}</p>;
  }
  if (reuse.casesQuery.error) {
    return <p className="text-sm text-destructive">{evaluationErrorMessage(reuse.casesQuery.error)}</p>;
  }
  if (reuse.cases.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {msg('evaluations.noCasesInVersion', 'This version has no cases yet.')}
      </p>
    );
  }
  const visibleIds = filtered.map((item) => item.id);
  const selectedVisible = visibleIds.filter((id) => reuse.selected.some((item) => item.id === id)).length;
  const allChecked = visibleIds.length > 0 && selectedVisible === visibleIds.length;
  return (
    <div className="grid gap-2">
      <label className="flex items-center gap-2 text-sm">
        <Checkbox
          className={checkedCaseClass}
          checked={selectedVisible > 0 && !allChecked ? 'indeterminate' : allChecked}
          onCheckedChange={(next) => {
            if (typeof next === 'boolean') {
              reuse.setMany(visibleIds, next);
            }
          }}
          aria-label={msg('evaluations.selectFiltered', 'Select current results ({count})', {
            count: filtered.length,
          })}
        />
        <span>{msg('evaluations.selectFiltered', 'Select current results ({count})', { count: filtered.length })}</span>
      </label>
      <ul className="max-h-64 space-y-1 overflow-y-auto">
        {filtered.map((item) => (
          <li key={item.id} className="flex items-start gap-2 py-1 text-sm">
            <Checkbox
              id={`reuse-case-${item.id}`}
              className={`mt-0.5 ${checkedCaseClass}`}
              checked={reuse.selected.some((entry) => entry.id === item.id)}
              onCheckedChange={(next) => {
                if (typeof next === 'boolean') {
                  reuse.setSelected(item.id, next);
                }
              }}
              aria-label={item.name}
            />
            <label htmlFor={`reuse-case-${item.id}`} className="min-w-0 flex-1">
              <span className="block truncate font-medium">{item.name}</span>
              <span className="block truncate text-xs text-muted-foreground">{item.turns[0] ?? ''}</span>
            </label>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 shrink-0 px-2 text-muted-foreground"
              onClick={() => onDetail(item)}
            >
              {msg('evaluations.viewDetail', 'View')}
            </Button>
          </li>
        ))}
      </ul>
      {reuse.selected.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.selectCasesRequired', 'Select at least one case.')}
        </p>
      ) : null}
    </div>
  );
}

function SelectedCases({ reuse }: { reuse: ReturnType<typeof useDatasetReuse> }) {
  const { msg } = useI18n();
  const version = reuse.version?.version ?? '';
  const source = reuse.option?.name ?? '';
  return (
    <section className="grid gap-2 rounded-lg border p-3">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm font-medium">
          {msg('evaluations.selectedCases', 'Selected {count}', { count: reuse.selected.length })}
        </p>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          disabled={reuse.selected.length === 0}
          onClick={reuse.clearSelected}
        >
          {msg('evaluations.clearSelected', 'Clear')}
        </Button>
      </div>
      <ol className="max-h-72 space-y-2 overflow-y-auto">
        {reuse.selected.map((item, index) => (
          <li key={item.id} className="flex items-start justify-between gap-2 border-b py-2 text-sm last:border-b-0">
            <div className="min-w-0">
              <p className="truncate font-medium">
                {index + 1}. {item.name}
              </p>
              <p className="truncate text-xs text-muted-foreground">
                {source} / v{version}
              </p>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 shrink-0 px-2 text-muted-foreground"
              onClick={() => reuse.setSelected(item.id, false)}
            >
              {msg('evaluations.removeSelected', 'Remove')}
            </Button>
          </li>
        ))}
      </ol>
    </section>
  );
}

function ReuseSelects({
  options,
  option,
  versionId,
  onSource,
  onVersion,
}: {
  options: ReuseOption[];
  option: ReuseOption;
  versionId: string;
  onSource: (key: string) => void;
  onVersion: (id: string) => void;
}) {
  const { msg } = useI18n();
  const presetMark = msg('evaluations.presetMark', 'Preset');
  const sourceItems = options.map((item) => ({
    value: item.key,
    label: item.kind === 'preset' ? `${item.name} · ${presetMark}` : item.name,
  }));
  const versionItems = option.versions.map((item) => ({
    value: item.id,
    label: msg('evaluations.versionCaseCount', 'v{version} · {count} cases', {
      version: item.version,
      count: item.caseCount,
    }),
  }));
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      <Select
        value={option.key}
        items={sourceItems}
        onValueChange={(next) => {
          if (next) {
            onSource(next);
          }
        }}
      >
        <SelectTrigger aria-label={msg('evaluations.sourceDataset', 'Source dataset')} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {sourceItems.map((item) => (
            <SelectItem key={item.value} value={item.value} label={item.label}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        value={versionId}
        items={versionItems}
        onValueChange={(next) => {
          if (next) {
            onVersion(next);
          }
        }}
      >
        <SelectTrigger aria-label={msg('evaluations.sourceVersion', 'Available version')} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {versionItems.map((item) => (
            <SelectItem key={item.value} value={item.value} label={item.label}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function CasePreview({
  item,
  source,
  version,
  onClose,
}: {
  item: EvaluationCase | null;
  source: string;
  version: number;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const checkpoints = checkpointLines(item?.checkpoints);
  return (
    <Dialog open={item !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{item?.name ?? msg('evaluations.viewDetail', 'View')}</DialogTitle>
        </DialogHeader>
        <p className="text-xs text-muted-foreground">
          {source} / v{version}
        </p>
        <ol className="grid gap-2">
          {(item?.turns ?? []).map((turn, index) => (
            <li key={`${index}-${turn}`} className="rounded-md bg-muted px-3 py-2 text-sm">
              {index + 1}. {turn}
            </li>
          ))}
        </ol>
        {checkpoints.length > 0 ? (
          <ul className="grid gap-1 text-sm text-muted-foreground">
            {checkpoints.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function checkpointLines(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is string => typeof item === 'string' && item.length > 0);
}

function matchesCase(query: string, item: EvaluationCase) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return [item.name, item.desc, ...item.turns].some((part) => part.toLowerCase().includes(needle));
}

function selectedOrder(cases: EvaluationCase[], ids: string[]) {
  return ids.flatMap((id) => {
    const item = cases.find((entry) => entry.id === id);
    return item ? [item] : [];
  });
}
