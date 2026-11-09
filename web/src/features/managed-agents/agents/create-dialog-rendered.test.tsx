import '../../../test/setup';
import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '../../../shared/i18n';
import { blankAgentTemplate, createDialogAgentConfig } from '../agentConfig';
import { AgentConfigRenderedEditor } from './create-dialog-rendered';

const { cleanup, fireEvent, render, screen, within, waitFor } = await import('@testing-library/react');
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  for (const client of clients) client.clear();
  clients.length = 0;
});

function mountModelPicker(value = 'provider/model-one') {
  const client = new QueryClient({ defaultOptions: { queries: { enabled: false, retry: false } } });
  clients.push(client);
  const onChange = mock();
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <AgentConfigRenderedEditor
          workspaceId="sample"
          draft={createDialogAgentConfig(blankAgentTemplate, 'en', undefined, value)}
          modelOptions={[
            { id: 'provider/model-one', displayName: 'provider/model-one' },
            { id: 'provider/model-two', displayName: 'Model Two' },
          ]}
          onChange={onChange}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'Model' }));
  return onChange;
}

test('shows identical model name and ID once while retaining distinct names and IDs', () => {
  mountModelPicker();
  const single = screen.getByRole('option', { name: 'provider/model-one' });
  expect(within(single).getAllByText('provider/model-one')).toHaveLength(1);
  const distinct = screen.getByRole('option', { name: 'Model Two provider/model-two' });
  expect(within(distinct).getByText('Model Two')).toBeTruthy();
  expect(within(distinct).getByText('provider/model-two')).toBeTruthy();
});

test('keeps unavailable selected models and searches and selects using the model ID', async () => {
  const onChange = mountModelPicker('provider/unavailable');
  expect(screen.getByRole('option', { name: 'provider/unavailable' })).toBeTruthy();
  fireEvent.change(screen.getByRole('combobox', { name: 'Search models...' }), {
    target: { value: 'provider/model-two' },
  });
  fireEvent.click(screen.getByRole('option', { name: 'Model Two provider/model-two' }));
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ model: 'provider/model-two' }));
  await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
});
