import type { ContentMode, DestinationInput, TelemetryDestination, WorkspaceScope } from './api';

export const DEFAULT_MAX_CONTENT_BYTES = 786432;
const BYTES_PER_MB = 1 << 20;
export const DEFAULT_MAX_CONTENT_MB = megabytes(DEFAULT_MAX_CONTENT_BYTES);

export interface HeaderRow {
  name: string;
  value: string;
}

export interface DestinationForm {
  displayName: string;
  endpointUrl: string;
  tracesUrl: string;
  contentMode: ContentMode;
  maxContentMb: string;
  scope: WorkspaceScope;
  workspaceIds: string[];
  workspacesChanged: boolean;
  replaceHeaders: boolean;
  headers: HeaderRow[];
}

export function emptyForm(): DestinationForm {
  return {
    displayName: '',
    endpointUrl: '',
    tracesUrl: '',
    contentMode: 'metadata_only',
    maxContentMb: DEFAULT_MAX_CONTENT_MB,
    scope: 'all',
    workspaceIds: [],
    workspacesChanged: true,
    replaceHeaders: true,
    headers: [{ name: '', value: '' }],
  };
}

function megabytes(bytes: number) {
  return String(Number((bytes / BYTES_PER_MB).toFixed(6)));
}

function contentBytes(form: DestinationForm) {
  return form.maxContentMb.trim() ? Math.round(Number(form.maxContentMb) * BYTES_PER_MB) : DEFAULT_MAX_CONTENT_BYTES;
}

export function formFromDestination(destination: TelemetryDestination): DestinationForm {
  const defaultTraces = `${destination.endpoint_url.replace(/\/+$/, '')}/v1/traces`;
  return {
    displayName: destination.display_name ?? '',
    endpointUrl: destination.endpoint_url,
    tracesUrl: destination.traces.endpoint_url === defaultTraces ? '' : destination.traces.endpoint_url,
    contentMode: destination.traces.content_mode,
    maxContentMb: megabytes(destination.traces.max_content_bytes),
    scope: destination.workspaces.type,
    workspaceIds: destination.workspaces.workspace_ids,
    workspacesChanged: false,
    replaceHeaders: false,
    headers: [],
  };
}

export function formErrors(form: DestinationForm) {
  const bytes = contentBytes(form);
  return {
    endpointUrl: !/^https?:\/\/\S+$/.test(form.endpointUrl.trim()),
    maxContentMb: !Number.isInteger(bytes) || bytes < 1 || bytes > DEFAULT_MAX_CONTENT_BYTES,
    workspaces: form.workspacesChanged && form.scope === 'include' && form.workspaceIds.length === 0,
    headers: form.replaceHeaders && form.headers.some((row) => !row.name.trim() && row.value),
  };
}

export function hasErrors(form: DestinationForm) {
  return Object.values(formErrors(form)).some(Boolean);
}

export function inputFromForm(form: DestinationForm): DestinationInput {
  const input: DestinationInput = {
    display_name: form.displayName.trim() || null,
    endpoint_url: form.endpointUrl.trim(),
    traces: {
      content_mode: form.contentMode,
      endpoint_url: form.tracesUrl.trim() || null,
      max_content_bytes: contentBytes(form),
    },
  };
  if (form.workspacesChanged) {
    input.workspaces = { type: form.scope, workspace_ids: form.scope === 'all' ? [] : form.workspaceIds };
  }
  if (form.replaceHeaders) {
    const rows = form.headers.filter((row) => row.name.trim());
    input.headers = rows.length ? Object.fromEntries(rows.map((row) => [row.name.trim(), row.value])) : null;
  }
  return input;
}
