import { expect, test } from 'bun:test';
import type { TelemetryDestination } from './api';
import { emptyForm, formErrors, formFromDestination, hasErrors, inputFromForm } from './form-model';

const destination: TelemetryDestination = {
  id: 'tdst_1',
  type: 'telemetry_destination',
  display_name: 'Collector',
  endpoint_url: 'https://otlp.example.com/',
  header_names: ['Authorization'],
  state: { type: 'enabled' },
  traces: {
    enabled: true,
    content_mode: 'content',
    endpoint_url: 'https://otlp.example.com/v1/traces',
    max_content_bytes: 1000,
  },
  workspaces: { type: 'include', workspace_ids: ['wrkspc_a'] },
  delivery_health: { last_error: null, last_error_at: null, last_success_at: null },
  created_at: '',
  updated_at: '',
};

test('invalid forms are rejected before submission', () => {
  const form = { ...emptyForm(), endpointUrl: 'not a url', maxContentMb: '0.8', scope: 'include' as const };
  form.headers = [{ name: ' ', value: 'secret' }];
  expect(formErrors(form)).toEqual({ endpointUrl: true, maxContentMb: true, workspaces: true, headers: true });
  expect(formErrors({ ...emptyForm(), maxContentMb: 'abc' }).maxContentMb).toBe(true);
  expect(emptyForm().headers).toEqual([{ name: '', value: '' }]);
  expect(hasErrors({ ...emptyForm(), endpointUrl: 'https://otlp.example.com' })).toBe(false);
});

test('creating sends headers and defaults while editing keeps stored headers unless replaced', () => {
  const created = inputFromForm({
    ...emptyForm(),
    displayName: '  ',
    endpointUrl: ' https://otlp.example.com ',
    headers: [
      { name: ' x-api-key ', value: 'secret' },
      { name: '', value: '' },
    ],
  });
  expect(created).toEqual({
    display_name: null,
    endpoint_url: 'https://otlp.example.com',
    traces: { content_mode: 'metadata_only', endpoint_url: null, max_content_bytes: 786432 },
    workspaces: { type: 'all', workspace_ids: [] },
    headers: { 'x-api-key': 'secret' },
  });
  const edit = formFromDestination(destination);
  expect(edit.tracesUrl).toBe('');
  expect(inputFromForm(edit).traces?.max_content_bytes).toBe(1000);
  expect(inputFromForm({ ...edit, maxContentMb: '' }).traces?.max_content_bytes).toBe(786432);
  expect(emptyForm().maxContentMb).toBe('0.75');
  expect(edit.replaceHeaders).toBe(false);
  expect(inputFromForm(edit)).not.toHaveProperty('headers');
  expect(inputFromForm({ ...edit, replaceHeaders: true }).headers).toBeNull();
  expect(inputFromForm(edit)).not.toHaveProperty('workspaces');
  expect(inputFromForm({ ...edit, scope: 'all', workspacesChanged: true }).workspaces).toEqual({
    type: 'all',
    workspace_ids: [],
  });
  const archived = formFromDestination({ ...destination, workspaces: { type: 'include', workspace_ids: [] } });
  expect(hasErrors(archived)).toBe(false);
  expect(inputFromForm(archived)).not.toHaveProperty('workspaces');
  const custom = formFromDestination({
    ...destination,
    traces: { ...destination.traces, endpoint_url: 'https://otlp.example.com/custom' },
  });
  expect(custom.tracesUrl).toBe('https://otlp.example.com/custom');
});
