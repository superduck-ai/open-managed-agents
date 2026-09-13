import type { SessionResourceApiResponse } from '../types';

const EMPTY = '—';

function stringField(value: unknown) {
  return typeof value === 'string' && value ? value : '';
}

export function sessionResourceDisplayName(
  resource: SessionResourceApiResponse,
  filenamesByFileId: Record<string, string>,
) {
  if (resource.type === 'memory_store') {
    return stringField(resource.name) || EMPTY;
  }
  if (resource.type === 'github_repository') {
    return stringField(resource.url) || EMPTY;
  }
  const fileId = stringField(resource.file_id);
  if (fileId) {
    return filenamesByFileId[fileId] || EMPTY;
  }
  return EMPTY;
}

export function sessionResourceIdentity(resource: SessionResourceApiResponse) {
  if (resource.type === 'memory_store') {
    return stringField(resource.memory_store_id) || EMPTY;
  }
  if (resource.type === 'github_repository') {
    return stringField(resource.url) || EMPTY;
  }
  return stringField(resource.file_id) || EMPTY;
}
