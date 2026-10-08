import type { EnvironmentApiResponse, EnvironmentEditValues } from '../types';
import { environmentEditValues, environmentConfigBody, environmentMetadataBody } from '../resources/model';
import { environmentMetadataPatch } from '../resources/environment-update';
import { objectRecord } from '../utils';

export type EnvironmentFormValues = EnvironmentEditValues & { hosting: 'cloud' | 'self_hosted' };

export const packageManagers = [
  { id: 'pip', name: 'Python', registry: 'PyPI', placeholder: 'pandas==2.2.0' },
  { id: 'npm', name: 'Node.js', registry: 'npm', placeholder: 'typescript@5.7.3' },
  { id: 'apt', name: 'System', registry: 'apt', placeholder: 'ffmpeg' },
  { id: 'cargo', name: 'Rust', registry: 'crates.io', placeholder: 'ripgrep' },
  { id: 'gem', name: 'Ruby', registry: 'RubyGems', placeholder: 'rails' },
  { id: 'go', name: 'Go', registry: 'go', placeholder: 'golang.org/x/tools/gopls@latest' },
] as const;

export function environmentFormValues(entity?: EnvironmentApiResponse): EnvironmentFormValues {
  if (!entity)
    return {
      name: '',
      description: '',
      hosting: 'cloud',
      networkType: 'limited',
      allowMcpServers: false,
      allowPackageManagers: false,
      allowedHostsText: '',
      packages: [],
      metadataRows: [],
    };
  const values = environmentEditValues(entity);
  return {
    ...values,
    hosting: objectRecord(entity.config).type === 'self_hosted' ? 'self_hosted' : 'cloud',
    packages: values.packages.filter((row) => row.value),
    metadataRows: values.metadataRows.filter((row) => row.key || row.value),
  };
}

export function environmentFormBody(
  values: EnvironmentFormValues,
  entity?: EnvironmentApiResponse,
  initial?: EnvironmentFormValues,
) {
  return {
    name: values.name.trim(),
    description: values.description.trim(),
    metadata: initial ? environmentMetadataPatch(values, initial) : environmentMetadataBody(values),
    config:
      entity && objectRecord(entity.config).type === 'self_hosted'
        ? { type: 'self_hosted' }
        : {
            ...objectRecord(entity?.config),
            ...environmentConfigBody(values),
          },
  };
}

export function environmentDraftKey(values: EnvironmentFormValues) {
  const body = environmentFormBody(values);
  return JSON.stringify({
    ...body,
    metadata: Object.fromEntries(Object.entries(body.metadata).sort(([a], [b]) => a.localeCompare(b))),
  });
}

export function addPackage(values: EnvironmentFormValues, manager: string, value: string): EnvironmentFormValues {
  if (values.packages.some((row) => row.manager === manager && row.value === value)) return values;
  return { ...values, packages: [...values.packages, { manager, value }] };
}
