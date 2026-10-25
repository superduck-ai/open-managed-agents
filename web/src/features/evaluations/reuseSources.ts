import type { CreateEvaluationDatasetInput, EvaluationDataset, EvaluationDatasetPreset } from './api';

export type ReuseKind = 'dataset' | 'preset';

export type ReuseOption = {
  key: string;
  kind: ReuseKind;
  id: string;
  name: string;
  versions: Array<{ id: string; version: number }>;
};

export function reuseOptions(datasets: EvaluationDataset[], presets: EvaluationDatasetPreset[]): ReuseOption[] {
  const custom = datasets
    .filter((dataset) => dataset.status === 'available' && dataset.version?.id)
    .map((dataset) => ({
      key: `dataset:${dataset.id}`,
      kind: 'dataset' as const,
      id: dataset.id,
      name: dataset.name,
      versions: [{ id: dataset.version.id, version: dataset.version.version }],
    }));
  const published = presets
    .filter((preset) => preset.status === 'available' && Array.isArray(preset.versions) && preset.versions.length > 0)
    .map((preset) => ({
      key: `preset:${preset.id}`,
      kind: 'preset' as const,
      id: preset.id,
      name: preset.name,
      versions: preset.versions.map((version) => ({ id: version.id, version: version.version })),
    }));
  return [...custom, ...published];
}

export function selectedReuseOption(options: ReuseOption[], sourceKey: string) {
  return options.find((option) => option.key === sourceKey) ?? options[0];
}

export function selectedReuseVersion(option: ReuseOption | undefined, versionId: string) {
  const versions = option?.versions ?? [];
  return versions.find((version) => version.id === versionId) ?? versions[versions.length - 1];
}

export function setCaseSelected(ids: string[], id: string, selected: boolean) {
  if (selected) {
    return ids.includes(id) ? ids : [...ids, id];
  }
  return ids.filter((item) => item !== id);
}

export function datasetCreateInput(
  source: 'upload' | 'reuse',
  name: string,
  desc: string,
  reuse: { kind: ReuseKind; id: string; versionId: string; caseIds: string[] } | null,
): CreateEvaluationDatasetInput {
  if (source !== 'reuse' || !reuse) {
    return { name, desc };
  }
  if (reuse.kind === 'preset') {
    return { name, desc, source_version_id: reuse.versionId, case_ids: reuse.caseIds };
  }
  return { name, desc, source_dataset_id: reuse.id, case_ids: reuse.caseIds };
}
