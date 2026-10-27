import { useQuery } from '@tanstack/react-query';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import type { EnvironmentEditValues, EnvironmentPackageRow } from '../types';
import { environmentTemplateBody } from '../resources/model';
import { environmentQueryKey } from './data';
import { packageManagers } from './model';

export type PrebuildStage = 'image' | 'template';
export type PrebuildState =
  'idle' | 'queued' | 'submitting' | 'running' | 'canceling' | 'cancelled' | 'failed' | 'unknown' | 'ready' | 'skipped';

type PrebuildRecord = {
  job_id: string;
  stage: PrebuildStage | '';
  state: PrebuildState;
  message: string;
  can_start: boolean;
  can_cancel: boolean;
  image_logs: boolean;
  template_logs: boolean;
  image_skipped?: boolean;
  created_at?: string | null;
  finished_at?: string | null;
};
type PrebuildResponse = { build: PrebuildRecord | null };

export type EnvironmentPrebuild = NonNullable<ReturnType<typeof parsePrebuild>>;

function parsePrebuild({ build: prebuild }: PrebuildResponse) {
  return (
    prebuild && {
      createdAt: prebuild.created_at ?? null,
      finishedAt: prebuild.finished_at ?? null,
      jobId: prebuild.job_id,
      stage: prebuild.stage,
      state: prebuild.state,
      message: prebuild.message,
      canStart: prebuild.can_start,
      canCancel: prebuild.can_cancel,
      hasImageLogs: prebuild.image_logs,
      hasTemplateLogs: prebuild.template_logs,
      imageSkipped: prebuild.image_skipped === true,
    }
  );
}

export function packagesKey(packages: EnvironmentPackageRow[]) {
  return JSON.stringify(
    packageManagers.map(({ id }) =>
      packages
        .filter((row) => row.manager === id)
        .map((row) => row.value.trim())
        .filter(Boolean),
    ),
  );
}

export function buildSpecKey(values?: EnvironmentEditValues) {
  if (!values) return '';
  const packages = values.packages.filter((row) => row.value.trim());
  const template = environmentTemplateBody(values);
  if (!packages.length && !template.init_script && !template.resources) return '';
  return JSON.stringify([packagesKey(packages), template.init_script, template.resources]);
}

export function prebuildIsActive(state?: PrebuildState) {
  return ['queued', 'submitting', 'running', 'canceling'].includes(state ?? '');
}

export function prebuildStepState(prebuild: EnvironmentPrebuild, stage: PrebuildStage): PrebuildState {
  if (stage === 'image' && prebuild.imageSkipped) return 'skipped';
  if (prebuild.state === 'ready' || (stage === 'image' && prebuild.stage === 'template')) return 'ready';
  if ((prebuild.stage || 'image') !== stage) return 'idle';
  return prebuild.state;
}

export function useEnvironmentPrebuild(workspaceId: string, environmentId: string | undefined, savedSpecKey: string) {
  return useQuery({
    queryKey: [...environmentQueryKey(workspaceId), 'prebuild', environmentId, savedSpecKey],
    queryFn: async ({ signal }) =>
      parsePrebuild(
        await anthropicBetaApi.environments.prebuild.retrieve<PrebuildResponse>(environmentId!, workspaceId, signal),
      ),
    enabled: Boolean(environmentId && savedSpecKey),
    retry: false,
    refetchInterval: (query) => (!query.state.error && prebuildIsActive(query.state.data?.state) ? 3000 : false),
  });
}
