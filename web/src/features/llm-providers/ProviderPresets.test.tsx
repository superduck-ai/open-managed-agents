import { afterEach, expect, mock, test } from 'bun:test';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { ProviderFormDialog } from './ProviderFormDialog';
import type { LLMProviderInput, ProviderPresetCatalog } from './api';
const { cleanup, fireEvent, render, screen, within, waitFor } = await import('@testing-library/react');
afterEach(cleanup);

const catalog: ProviderPresetCatalog = {
  providers: [
    {
      id: 'gateway',
      name: 'Gateway',
      api_backend: 'chat_completions',
      base_url: 'https://gateway.example.test',
      models: [],
    },
  ],
};

function renderPreset() {
  const save = mock(async (_input: LLMProviderInput) => true);
  render(
    <I18nProvider initialLocale="en">
      <ProviderFormDialog
        provider={null}
        error=""
        isPending={false}
        onClose={() => {}}
        onSubmit={save}
        onDiscoverModels={async () => ['second', 'first']}
        presetCatalog={catalog}
        onReloadPresets={() => {}}
      />
    </I18nProvider>,
  );
  return { save };
}

async function selectGateway() {
  fireEvent.click(screen.getByRole('combobox', { name: 'Model supplier' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Gateway' }));
  fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'test-key' } });
}

test('loads empty supplier models, selects fetched models, saves only the connection', async () => {
  const calls = renderPreset();
  expect(screen.getByRole('combobox', { name: 'Model supplier' }).textContent).toContain('Select a supplier');
  await selectGateway();
  expect(screen.queryByLabelText('Base URL')).toBeNull();
  expect(screen.queryByRole('checkbox', { name: /all organizations/ })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Custom settings' }));
  expect(screen.getByLabelText('API key').getAttribute('placeholder')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Get available models' }));
  const candidates = await screen.findByRole('dialog', { name: 'Select models' });
  expect(screen.getAllByRole('dialog')).toHaveLength(1);
  expect(screen.getByLabelText('API key').closest('form')?.hidden).toBe(true);
  fireEvent.click(within(candidates).getByRole('button', { name: 'Add selected models' }));
  expect(screen.getByLabelText('Model ID 1')).toHaveProperty('value', 'second');
  fireEvent.change(screen.getByLabelText('Display name 1'), { target: { value: 'Second model' } });
  fireEvent.click(screen.getByRole('button', { name: 'Model parameters 1' }));
  fireEvent.change(screen.getByLabelText('Context window 1'), { target: { value: '128K' } });
  expect(screen.queryByRole('button', { name: 'Move model down 1' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(calls.save).toHaveBeenCalledTimes(1));
  const input = calls.save.mock.calls[0][0];
  expect(input.models.map((model) => model.id)).toEqual(['second', 'first']);
  expect(input.models[0]).toEqual({
    id: 'second',
    name: 'Second model',
    context_window: 128000,
    input_modalities: ['text'],
  });
});

test('can save an empty configured supplier without fetching any models', async () => {
  const calls = renderPreset();
  await selectGateway();
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(calls.save).toHaveBeenCalledTimes(1));
  expect(calls.save.mock.calls[0][0].models).toEqual([]);
});

test('marks only the missing supplier API key and focuses its input', async () => {
  const calls = renderPreset();
  fireEvent.click(screen.getByRole('combobox', { name: 'Model supplier' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Gateway' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  const key = screen.getByLabelText('API key');
  expect(key.getAttribute('aria-invalid')).toBe('true');
  expect(await screen.findByText('API key is required.')).toBeTruthy();
  expect(document.activeElement).toBe(key);
  expect(screen.queryByText('Display name is required.')).toBeNull();
  fireEvent.change(key, { target: { value: 'test-key' } });
  expect(key.getAttribute('aria-invalid')).toBe('false');
  expect(screen.queryByText('API key is required.')).toBeNull();
  expect(calls.save).not.toHaveBeenCalled();
});

test('shows field-specific custom API errors and keeps model display names optional', async () => {
  renderPreset();
  fireEvent.click(screen.getByRole('tab', { name: 'Custom model API' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  for (const label of ['Provider ID', 'Base URL', 'API key']) {
    expect(screen.getByLabelText(label).getAttribute('aria-invalid')).toBe('true');
  }
  expect(screen.getByLabelText('Display name 1').getAttribute('aria-invalid')).not.toBe('true');
  expect(screen.getByText('Display name is optional. Leave it blank to use the model ID.')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Model ID 1'), { target: { value: 'model-without-name' } });
  expect(screen.getByLabelText('Display name 1').getAttribute('placeholder')).toBe('model-without-name');
});

test('groups advanced fields in the disclosure and preserves their draft when reopened', async () => {
  renderPreset();
  await selectGateway();
  const trigger = screen.getByRole('button', { name: 'Custom settings' });
  expect(trigger.getAttribute('aria-expanded')).toBe('false');
  expect(screen.queryByRole('button', { name: 'Get available models' })).toBeNull();
  fireEvent.click(trigger);
  const panel = document.getElementById(trigger.getAttribute('aria-controls')!);
  expect(panel).toBeTruthy();
  expect(panel!.contains(screen.getByLabelText('Base URL'))).toBe(true);
  expect(panel!.contains(screen.getByRole('button', { name: 'Get available models' }))).toBe(true);
  expect(panel!.contains(screen.getByLabelText('API key'))).toBe(false);
  expect(screen.queryByRole('checkbox', { name: /all organizations/ })).toBeNull();
  fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: 'https://edited.example.test' } });
  fireEvent.click(trigger);
  expect(trigger.getAttribute('aria-expanded')).toBe('false');
  expect(screen.getByLabelText('API key')).toHaveProperty('value', 'test-key');
  expect(screen.queryByRole('checkbox', { name: /all organizations/ })).toBeNull();
  fireEvent.click(trigger);
  expect(screen.getByLabelText('Base URL')).toHaveProperty('value', 'https://edited.example.test');
});

test('opens advanced settings and focuses an invalid hidden connection field', async () => {
  const calls = renderPreset();
  await selectGateway();
  const trigger = screen.getByRole('button', { name: 'Custom settings' });
  fireEvent.click(trigger);
  fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: '' } });
  fireEvent.click(trigger);
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(trigger.getAttribute('aria-expanded')).toBe('true'));
  expect(screen.getByLabelText('Base URL').getAttribute('aria-invalid')).toBe('true');
  await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText('Base URL')));
  expect(calls.save).not.toHaveBeenCalled();
});

test('cancels discovery with Escape, restores draft and focus in the provider form', async () => {
  renderPreset();
  await selectGateway();
  fireEvent.click(screen.getByRole('button', { name: 'Custom settings' }));
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'My gateway' } });
  fireEvent.click(screen.getByRole('button', { name: 'Get available models' }));
  const dialog = await screen.findByRole('dialog', { name: 'Select models' });
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  await screen.findByRole('dialog', { name: 'Add provider' });
  expect(screen.getByLabelText('Display name')).toHaveProperty('value', 'My gateway');
  expect(screen.getByLabelText('API key')).toHaveProperty('value', 'test-key');
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Get available models' }));
});

test('custom provider name follows its ID until customized and clearing restores the default', async () => {
  const { save } = renderPreset();
  expect(screen.queryByText(/Choose a supplier to fill in/)).toBeNull();
  fireEvent.click(screen.getByRole('tab', { name: 'Custom model API', exact: true }));
  const id = screen.getByLabelText('Provider ID');
  const name = screen.getByLabelText('Display name');
  expect(id.getAttribute('placeholder')).toBe('deepseek');
  expect(screen.getByLabelText('Model ID 1').getAttribute('placeholder')).toBe('kimi-k3');
  expect(screen.queryByText(/Paste a base URL or a full/)).toBeNull();
  fireEvent.change(id, { target: { value: 'my-provider' } });
  expect(name).toHaveProperty('value', 'my-provider');
  fireEvent.change(id, { target: { value: 'another-provider' } });
  expect(name).toHaveProperty('value', 'another-provider');
  fireEvent.change(name, { target: { value: 'Research models' } });
  fireEvent.change(id, { target: { value: 'research' } });
  expect(name).toHaveProperty('value', 'Research models');
  fireEvent.change(name, { target: { value: '' } });
  expect(name).toHaveProperty('value', 'research');
  fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: 'https://example.test' } });
  fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'test-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
  expect(save.mock.calls[0][0].name).toBe('research');
});
