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
      description: values.description.trim(),
      config: environmentConfigBody(values),
      metadata: environmentMetadataPatch(values, initialValues),
    },
    workspaceId,
  );
}

function environmentMetadataPatch(values: EnvironmentEditValues, initialValues: EnvironmentEditValues) {
  const currentMetadata = environmentMetadataBody(values);
  const initialMetadata = environmentMetadataBody(initialValues);
  const metadata: Record<string, string | null> = {};
  for (const [key, value] of Object.entries(currentMetadata)) {
    if (initialMetadata[key] !== value) {
      metadata[key] = value;
    }
  }
  for (const key of Object.keys(initialMetadata)) {
    if (!Object.prototype.hasOwnProperty.call(currentMetadata, key)) {
      metadata[key] = null;
    }
  }
  return metadata;
}
