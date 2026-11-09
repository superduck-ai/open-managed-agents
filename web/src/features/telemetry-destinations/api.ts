import { telemetryDestinationsApi } from '../../shared/api/client';

export type ContentMode = 'metadata_only' | 'content';
export type WorkspaceScope = 'all' | 'include' | 'exclude';
export type DestinationState = { type: 'enabled' } | { type: 'disabled'; reason?: string };

export interface TelemetryDestination {
  id: string;
  type: 'telemetry_destination';
  display_name: string | null;
  endpoint_url: string;
  header_names: string[];
  state: DestinationState;
  traces: { enabled: boolean; content_mode: ContentMode; endpoint_url: string; max_content_bytes: number };
  workspaces: { type: WorkspaceScope; workspace_ids: string[] };
  delivery_health: { last_error: string | null; last_error_at: string | null; last_success_at: string | null };
  created_at: string;
  updated_at: string;
}

export interface DestinationInput {
  display_name?: string | null;
  endpoint_url?: string;
  headers?: Record<string, string> | null;
  traces?: { enabled?: boolean; content_mode?: ContentMode; endpoint_url?: string | null; max_content_bytes?: number };
  workspaces?: { type: WorkspaceScope; workspace_ids?: string[] };
  state?: { type: 'enabled' | 'disabled' };
}

export interface TestResult {
  type: 'telemetry_destination_test_result';
  success: boolean;
  status_code: number | null;
  error: string | null;
}

const BASE = '/v1/organizations/telemetry_destinations';

export async function listTelemetryDestinations(workspaceId?: string) {
  const filter = workspaceId ? `&workspace_ids[]=${encodeURIComponent(workspaceId)}` : '';
  const page = await telemetryDestinationsApi<{ data: TelemetryDestination[] }>(`${BASE}?limit=100${filter}`);
  return page.data;
}

export function createTelemetryDestination(input: DestinationInput) {
  return telemetryDestinationsApi<TelemetryDestination>(BASE, { method: 'POST', body: JSON.stringify(input) });
}

export function updateTelemetryDestination(id: string, input: DestinationInput) {
  return telemetryDestinationsApi<TelemetryDestination>(`${BASE}/${encodeURIComponent(id)}`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function deleteTelemetryDestination(id: string) {
  return telemetryDestinationsApi<{ id: string }>(`${BASE}/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export function testTelemetryDestination(id: string) {
  return telemetryDestinationsApi<TestResult>(`${BASE}/${encodeURIComponent(id)}/test`, { method: 'POST' });
}
