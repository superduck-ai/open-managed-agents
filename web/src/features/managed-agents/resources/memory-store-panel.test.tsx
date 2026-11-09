import '../../../test/setup';
import { afterEach, expect, mock, test } from 'bun:test';
import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';
import { MemoryStorePanel } from './detail';
import type { MemoryStoreApiResponse } from '../types';

const originalFetch = globalThis.fetch;
const store: MemoryStoreApiResponse = {
  id: 'memstore_fixture',
  type: 'memory_store',
  name: 'Research memories',
  description: 'Research notes',
  created_at: '2026-10-25T00:00:00Z',
};

function PanelHarness() {
  const [refreshKey, setRefreshKey] = useState(0);
  return (
    <I18nProvider initialLocale="en">
      <MemoryStorePanel
        store={store}
        workspaceId="wrkspc_fixture"
        refreshKey={refreshKey}
        onRefresh={() => setRefreshKey((value) => value + 1)}
        variant="page"
      />
    </I18nProvider>
  );
}

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('keeps directory failure distinct from an empty store and retries without mutations', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/wrkspc_fixture/memory-stores/memstore_fixture');
  const methods: string[] = [];
  globalThis.fetch = mock(async (_input, init) => {
    methods.push(init?.method ?? 'GET');
    return methods.length === 1
      ? Response.json({ error: { type: 'api_error', message: 'Directory unavailable' } }, { status: 503 })
      : Response.json({ data: [], has_more: false });
  });
  render(<PanelHarness />);
  await screen.findByRole('alert');
  expect(screen.queryByText('No memories yet')).toBeNull();
  expect(screen.queryByText('Select a memory')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await screen.findByText('No memories yet');
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  expect(methods).toEqual(['GET', 'GET']);
});
