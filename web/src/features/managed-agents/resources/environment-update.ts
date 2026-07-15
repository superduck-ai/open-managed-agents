import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { type EnvironmentApiResponse, type EnvironmentEditValues } from '../types';
import { environmentConfigBody, environmentMetadataBody } from './model';

export function updateEnvironmentDetail(
  environmentId: string,
  values: EnvironmentEditValues,
  initialValues: EnvironmentEditValues,
  workspaceId: string,
) {
  return anthropicBetaApi.environments.update<EnvironmentApiResponse>(
    environmentId,
    {
      name: values.name.trim(),
      description: values.description,
      config: environmentConfigBody(values),
      metadata: environmentMetadataPatch(values, initialValues),
    },
    workspaceId,
  );
}

function environmentMetadataPatch(values: EnvironmentEditValues, initialValues: EnvironmentEditValues) {
  const metadata: Record<string, string | null> = environmentMetadataBody(values);
  const initialMetadata = environmentMetadataBody(initialValues);
  for (const key of Object.keys(initialMetadata)) {
    if (!(key in metadata)) {
      metadata[key] = null;
    }
  }
  return metadata;
}
