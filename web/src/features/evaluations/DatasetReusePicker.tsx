import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { evaluationErrorMessage, type EvaluationCase } from './api';
import {
  reuseOptions,
  selectedReuseOption,
  selectedReuseVersion,
  setCaseSelected,
  type ReuseOption,
} from './reuseSources';
import {
  useEvaluationDatasetList,
  useEvaluationDatasetPresetList,
  useEvaluationReuseCases,
} from './useEvaluationDatasets';

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
    reset() {
      setSourceKey('');
      setVersionId('');
      setSelectedIds([]);
    },
  };
}

export function DatasetReusePicker({ reuse }: { reuse: ReturnType<typeof useDatasetReuse> }) {
  const { msg } = useI18n();
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
  return (
    <div className="grid gap-3">
      <p className="text-sm text-muted-foreground">
        {msg('evaluations.reuseHint', 'Pick cases from an existing dataset and combine them into a new one.')}
      </p>
      <ReuseSelects
        options={reuse.options}
        option={reuse.option}
        versionId={reuse.version.id}
        onSource={reuse.chooseSource}
        onVersion={reuse.chooseVersion}
      />
      <ReuseCaseBody reuse={reuse} />
    </div>
  );
}

function ReuseCaseBody({ reuse }: { reuse: ReturnType<typeof useDatasetReuse> }) {
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
  return (
    <>
      <ReuseCaseColumns cases={reuse.cases} selected={reuse.selected} onSelected={reuse.setSelected} />
      {reuse.selected.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.selectCasesRequired', 'Select at least one case.')}
        </p>
      ) : null}
    </>
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
  const versionItems = option.versions.map((item) => ({ value: item.id, label: `v${item.version}` }));
  return (
    <div className="grid gap-3 sm:grid-cols-2">
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

function ReuseCaseColumns({
  cases,
  selected,
  onSelected,
}: {
  cases: EvaluationCase[];
  selected: EvaluationCase[];
  onSelected: (id: string, selected: boolean) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <ul className="max-h-64 space-y-2 overflow-y-auto rounded-lg border p-2">
        {cases.map((item) => (
          <li key={item.id} className="flex items-start gap-2 text-sm">
            <Checkbox
              id={`reuse-case-${item.id}`}
              checked={selected.some((entry) => entry.id === item.id)}
              onCheckedChange={(next) => {
                if (typeof next === 'boolean') {
                  onSelected(item.id, next);
                }
              }}
              aria-label={item.name}
            />
            <label htmlFor={`reuse-case-${item.id}`} className="min-w-0">
              <span className="block font-medium">{item.name}</span>
              <span className="block truncate text-xs text-muted-foreground">{item.turns[0] ?? ''}</span>
            </label>
          </li>
        ))}
      </ul>
      <div className="max-h-64 overflow-y-auto rounded-lg border p-2">
        <p className="mb-2 text-sm font-medium">
          {msg('evaluations.selectedCases', 'Selected {count}', { count: selected.length })}
        </p>
        <ul className="space-y-2">
          {selected.map((item) => (
            <li key={item.id} className="flex items-center justify-between gap-2 text-sm">
              <span className="min-w-0 truncate">{item.name}</span>
              <Button type="button" variant="ghost" className="h-7 px-2" onClick={() => onSelected(item.id, false)}>
                {msg('evaluations.removeSelected', 'Remove')}
              </Button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function selectedOrder(cases: EvaluationCase[], ids: string[]) {
  return ids.flatMap((id) => {
    const item = cases.find((entry) => entry.id === id);
    return item ? [item] : [];
  });
}
