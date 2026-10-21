import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  RouterContextProvider,
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { useEffect, type ReactNode } from 'react';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import type { Locale } from '../../shared/i18n';
import { toast, Toaster } from '../../shared/ui/sonner';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { LLMModelsPage } from './LLMModelsPage';
import type { LLMProvider } from './api';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  toast.dismiss();
  globalThis.fetch = originalFetch;
  window.localStorage.clear();
  window.sessionStorage.clear();
});

describe('LLM models page', () => {
  test('blocks direct access for non-administrators without requesting provider data', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi();
    renderPage('en', 'developer');

    const accessDenied = await screen.findByTestId('llm-models-access-denied');
    expect(accessDenied.textContent).toContain('Administrator access required');
    expect(accessDenied.textContent).toContain('Only organization administrators can manage model configuration.');
    expect(screen.queryByRole('button', { name: 'Add provider' })).toBeNull();
    expect(api.requests).toHaveLength(0);
  });

  test('creates and edits a Chat Completions provider and discovers using its backend', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi(['chat-model']);
    renderPage();
    await screen.findByText('No providers yet');
    fireEvent.click(screen.getByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.click(within(dialog).getByRole('combobox', { name: 'API backend' }));
    const option = await screen.findByRole('option', { name: 'OpenAI Chat Completions' });
    fireEvent.pointerDown(option);
    fireEvent.mouseDown(option);
    fireEvent.pointerUp(option);
    fireEvent.mouseUp(option);
    fireEvent.click(option);
    expect(within(dialog).getByText('/v1/chat/completions')).toBeTruthy();
    expect(within(dialog).getByText('Chat Completions is supported in brain mode only.')).toBeTruthy();
    expect(screen.queryByRole('option', { name: 'Responses' })).toBeNull();
    fireEvent.change(within(dialog).getByLabelText('Provider name'), { target: { value: 'Chat Gateway' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://chat.example.com' } });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'chat-key' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Fetch model list' }));
    await waitFor(() =>
      expect((within(dialog).getByRole('textbox', { name: 'Model ID 1' }) as HTMLInputElement).value).toBe(
        'chat-model',
      ),
    );
    expect(api.requests.find((request) => request.url.endsWith('/preview_models'))?.body?.api_backend).toBe(
      'chat_completions',
    );
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await screen.findByRole('button', { name: 'Chat Gateway models' });
    expect(
      api.requests.find((request) => request.method === 'POST' && request.url.endsWith('/llm_providers'))?.body
        ?.api_backend,
    ).toBe('chat_completions');
    fireEvent.click(screen.getByRole('button', { name: 'Chat Gateway' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const editDialog = screen.getByRole('dialog', { name: 'Edit provider' });
    expect(within(editDialog).getByRole('combobox', { name: 'API backend' }).textContent).toContain(
      'OpenAI Chat Completions',
    );
    fireEvent.click(within(editDialog).getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(api.requests.find((request) => request.method === 'PUT')?.body?.api_backend).toBe('chat_completions'),
    );
  });

  test('shows the workspace provider empty state', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi();
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    expect(screen.getByText('Manage providers and models for this workspace.')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Add provider' })).toHaveLength(1);
    expect(
      screen.queryByText('Add a provider before using Workbench, Agents, Batches, or the Messages API.'),
    ).toBeNull();
  });

  test('keeps an empty-model provider visible through add, edit, model add, and delete', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi();
    renderPage();

    await screen.findByText('No providers yet');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add provider' })[0]);
    const createDialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.change(within(createDialog).getByLabelText('Provider name'), { target: { value: 'Empty Gateway' } });
    fireEvent.change(within(createDialog).getByLabelText('Base URL'), {
      target: { value: 'https://empty.example.com/anthropic' },
    });
    fireEvent.change(within(createDialog).getByLabelText('API key'), { target: { value: 'empty-test-key' } });
    fireEvent.click(within(createDialog).getByRole('button', { name: 'Fetch model list' }));
    expect(await screen.findByText('The provider returned no models. You can add model IDs later.')).toBeTruthy();
    fireEvent.click(within(createDialog).getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('button', { name: 'Empty Gateway models' })).toBeTruthy();
    expect(screen.getByText('No models configured for this provider.')).toBeTruthy();
    expect(screen.queryAllByTestId('llm-model-row')).toHaveLength(0);

    fireEvent.click(screen.getByRole('button', { name: 'Empty Gateway' }));
    expect(screen.getByText('No models configured for this provider.')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Add or search model'), { target: { value: 'glm-4.7' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add glm-4.7 to Empty Gateway' }));
    expect(await screen.findByText('empty-gateway/glm-4.7')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editDialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.change(within(editDialog).getByRole('textbox', { name: 'Model ID 1' }), { target: { value: '' } });
    fireEvent.click(within(editDialog).getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('No models configured for this provider.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const deleteDialog = screen.getByRole('alertdialog', { name: 'Delete provider?' });
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Delete' }));
    expect(await screen.findByText('No providers yet')).toBeTruthy();
  });

  test('creates multiple providers, preserves a blank edit key, and deletes a provider', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    window.localStorage.clear();
    window.sessionStorage.clear();
    const api = mockProvidersApi();
    renderPage();

    await screen.findByText('No providers yet');
    await createProvider('DashScope', 'https://dashscope.example.com/anthropic', 'test-secret-1111', [
      'kimi-k2.5',
      'qwen-max',
    ]);
    await createProvider('Moonshot', 'https://moonshot.example.com/anthropic', 'test-secret-2222', ['moonshot-v1']);

    expect(await screen.findByRole('button', { name: 'All providers' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'DashScope' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Moonshot' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'DashScope models' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Moonshot models' })).toBeTruthy();
    expect(screen.getByText('dashscope/kimi-k2.5')).toBeTruthy();
    expect(screen.getByText('dashscope/qwen-max')).toBeTruthy();
    expect(screen.getByText('moonshot/moonshot-v1')).toBeTruthy();
    expect(screen.getAllByTestId('llm-model-row')).toHaveLength(3);
    expect(
      api.requests
        .filter((request) => request.method === 'POST' && request.url.endsWith('/llm_providers'))
        .map((request) => request.body?.slug),
    ).toEqual(['dashscope', 'moonshot']);
    const groupedDashScope = screen.getByTestId('llm-provider-group-provider_1');
    expect(within(groupedDashScope).getByText('https://dashscope.example.com/anthropic')).toBeTruthy();
    expect(within(groupedDashScope).getByText('•••• 1111')).toBeTruthy();
    expect(within(groupedDashScope).getAllByTestId('llm-model-row')).toHaveLength(2);

    fireEvent.click(screen.getByRole('button', { name: 'DashScope models' }));
    expect(screen.getByRole('button', { name: 'DashScope models' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByRole('button', { name: 'Moonshot models' }).getAttribute('aria-expanded')).toBe('true');
    expect(api.requests.some((request) => request.url.includes('/v1/models'))).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: 'DashScope' }));
    const selectedDashScope = screen.getByTestId('llm-provider-group-provider_1');
    expect(within(selectedDashScope).getByText('https://dashscope.example.com/anthropic')).toBeTruthy();
    expect(within(selectedDashScope).getByText('•••• 1111')).toBeTruthy();
    expect(within(selectedDashScope).getAllByTestId('llm-model-row')).toHaveLength(2);
    expect(screen.queryByText('test-secret-1111')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editDialog = screen.getByRole('dialog', { name: 'Edit provider' });
    const editSlug = within(editDialog).getByLabelText('Slug') as HTMLInputElement;
    expect(editSlug.value).toBe('dashscope');
    expect(editSlug.readOnly).toBe(true);
    const editApiKey = within(editDialog).getByLabelText('API key') as HTMLInputElement;
    expect(editApiKey.value).toBe('');
    expect(within(editDialog).queryByRole('button', { name: 'Fetch model list' })).toBeNull();
    fireEvent.change(editApiKey, { target: { value: 'replacement-key' } });
    expect(within(editDialog).getByRole('button', { name: 'Fetch model list' })).toBeTruthy();
    fireEvent.change(editApiKey, { target: { value: '' } });
    expect(within(editDialog).queryByRole('button', { name: 'Fetch model list' })).toBeNull();
    fireEvent.change(within(editDialog).getByLabelText('Provider name'), {
      target: { value: 'DashScope updated' },
    });
    fireEvent.click(within(editDialog).getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('button', { name: 'DashScope updated' })).toBeTruthy();
    expect(api.requests.findLast((request) => request.method === 'PUT')?.body).not.toHaveProperty('api_key');
    expect(api.requests.findLast((request) => request.method === 'PUT')?.body?.slug).toBe('dashscope');

    fireEvent.click(screen.getByRole('button', { name: 'Moonshot' }));
    expect(screen.getByText('•••• 2222')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const deleteDialog = screen.getByRole('alertdialog', { name: 'Delete provider?' });
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(screen.queryByRole('button', { name: 'Moonshot' })).toBeNull());
    expect(screen.getByRole('button', { name: 'DashScope updated' })).toBeTruthy();
    expect(storageText(window.localStorage)).not.toContain('test-secret-1111');
    expect(storageText(window.sessionStorage)).not.toContain('test-secret-1111');
    expect(storageText(window.localStorage)).not.toContain('test-secret-2222');
    expect(storageText(window.sessionStorage)).not.toContain('test-secret-2222');
  }, 10_000);

  test('searches the provider catalog, allows a fuzzy-query model ID, and refreshes providers', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi();
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    await createProvider('DashScope', 'https://dashscope.example.com/anthropic', 'test-secret-1111', [
      'kimi-k2.5',
      'qwen-max',
    ]);
    fireEvent.click(screen.getByRole('button', { name: 'All providers' }));
    expect(screen.getByRole('button', { name: 'All providers' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'DashScope' }));
    await screen.findByText('dashscope/kimi-k2.5', {}, { timeout: 2000 });

    fireEvent.change(screen.getByLabelText('Add or search model'), { target: { value: 'kimi' } });
    expect(screen.getByText('dashscope/kimi-k2.5')).toBeTruthy();
    expect(screen.queryByText('dashscope/qwen-max')).toBeNull();
    expect(screen.getByRole('button', { name: 'Add kimi to DashScope' })).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Add or search model'), { target: { value: 'glm-4.7' } });
    fireEvent.click(await screen.findByRole('button', { name: 'Add glm-4.7 to DashScope' }, { timeout: 2000 }));
    await waitFor(
      () => {
        expect(screen.getByText('dashscope/glm-4.7')).toBeTruthy();
        expect(screen.getByText('dashscope/qwen-max')).toBeTruthy();
      },
      { timeout: 2000 },
    );
    const beforeSync = api.requests.filter((request) => request.url.includes('/models/sync')).length;
    fireEvent.click(screen.getByRole('button', { name: 'Refresh model list' }));
    await waitFor(
      () => {
        expect(api.requests.filter((request) => request.url.includes('/models/sync')).length).toBeGreaterThan(
          beforeSync,
        );
        expect(api.requests.some((request) => request.url.includes('/v1/models'))).toBe(false);
      },
      { timeout: 2000 },
    );
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Refresh model list' }) as HTMLButtonElement).disabled).toBe(false),
    );
  });

  test('shows a localized error and keeps the query when adding a model fails', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi([], {
      updateStatus: 400,
      updateCode: 'model_ids_limit',
      updateMessage: 'wording changed',
    });
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    await createProvider('DashScope', 'https://dashscope.example.com/anthropic', 'test-secret-1111', ['kimi-k2.5']);
    fireEvent.click(screen.getByRole('button', { name: 'DashScope' }));
    const search = screen.getByLabelText('Add or search model') as HTMLInputElement;
    fireEvent.change(search, { target: { value: 'glm-4.7' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add glm-4.7 to DashScope' }));

    const toastTitle = await screen.findByText('You can configure at most 100 model IDs.');
    expect(toastTitle.closest('[data-sonner-toast]')?.getAttribute('data-type')).toBe('error');
    expect(search.value).toBe('glm-4.7');
    expect(screen.queryByText('wording changed')).toBeNull();
  });

  test('fills model IDs from the provider after URL and key, and refresh syncs live models', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi(['glm-4.7', 'kimi-k2.5']);
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: 'Add provider' })[0]);
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.change(within(dialog).getByLabelText('Provider name'), { target: { value: 'DashScope' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Fetch model list' }));

    expect(await within(dialog).findByDisplayValue('glm-4.7', {}, { timeout: 2000 })).toBeTruthy();
    expect(within(dialog).getByDisplayValue('kimi-k2.5')).toBeTruthy();
    expect(api.requests.some((request) => request.url.endsWith('/preview_models'))).toBe(true);

    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('dashscope/glm-4.7', {}, { timeout: 2000 })).toBeTruthy();
    expect(api.requests.some((request) => request.url.includes('/models/sync'))).toBe(false);

    const beforeRefresh = api.requests.filter((request) => request.url.includes('/models/sync')).length;
    fireEvent.click(screen.getByRole('button', { name: 'Refresh model list' }));
    await waitFor(
      () =>
        expect(api.requests.filter((request) => request.url.includes('/models/sync')).length).toBeGreaterThan(
          beforeRefresh,
        ),
      { timeout: 2000 },
    );
  });

  test('merges fetched models with manually entered model IDs', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi(['glm-4.7', 'kimi-k2.5']);
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Model ID 1' }), {
      target: { value: 'manual-model' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Fetch model list' }));

    expect(await within(dialog).findByDisplayValue('manual-model')).toBeTruthy();
    expect(within(dialog).getByDisplayValue('glm-4.7')).toBeTruthy();
    expect(within(dialog).getByDisplayValue('kimi-k2.5')).toBeTruthy();
  });

  test('keeps manually entered model IDs when fetching returns no models', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi();
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Model ID 1' }), {
      target: { value: 'manual-model' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Fetch model list' }));

    expect(await screen.findByText('The provider returned no models. You can add model IDs later.')).toBeTruthy();
    expect(within(dialog).getByDisplayValue('manual-model')).toBeTruthy();
  });

  test('disables model addition while the update is pending', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    let finishUpdate = () => {};
    const updateGate = new Promise<void>((resolve) => {
      finishUpdate = resolve;
    });
    mockProvidersApi([], { updateGate });
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    await createProvider('DashScope', 'https://dashscope.example.com/anthropic', 'test-secret-1111', ['kimi-k2.5']);
    fireEvent.click(screen.getByRole('button', { name: 'DashScope' }));
    fireEvent.change(screen.getByLabelText('Add or search model'), { target: { value: 'glm-4.7' } });
    const addButton = screen.getByRole('button', { name: 'Add glm-4.7 to DashScope' }) as HTMLButtonElement;
    fireEvent.click(addButton);

    await waitFor(() => expect(addButton.disabled).toBe(true));
    finishUpdate();
    expect(await screen.findByText('dashscope/glm-4.7')).toBeTruthy();
  });

  test('adds a model ID that another provider already serves', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi();
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    await createProvider('DashScope', 'https://dashscope.example.com/anthropic', 'test-secret-1111', ['kimi-k2.5']);
    await createProvider('Moonshot', 'https://moonshot.example.com/anthropic', 'test-secret-2222', ['moonshot-v1']);
    fireEvent.click(screen.getByRole('button', { name: 'Moonshot' }));
    fireEvent.change(screen.getByLabelText('Add or search model'), { target: { value: 'kimi-k2.5' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add kimi-k2.5 to Moonshot' }));

    expect(await screen.findByText('moonshot/kimi-k2.5')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Add or search model'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'All providers' }));
    expect(screen.getByText('dashscope/kimi-k2.5')).toBeTruthy();
    expect(screen.getByText('moonshot/kimi-k2.5')).toBeTruthy();
  });

  test('rejects an invalid slug before saving a provider', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi();
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: 'Add provider' })[0]);
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.change(within(dialog).getByLabelText('Provider name'), { target: { value: 'DashScope CN' } });
    expect((within(dialog).getByLabelText('Slug') as HTMLInputElement).value).toBe('dashscope-cn');
    fireEvent.change(within(dialog).getByLabelText('Slug'), { target: { value: 'Dash/Scope' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    expect(
      await within(dialog).findByText(
        'Slug must contain 1 to 32 lowercase letters, digits, ".", "_" or "-" and start with a letter or digit.',
      ),
    ).toBeTruthy();
    expect(api.requests.some((request) => request.method === 'POST' && request.url.endsWith('/llm_providers'))).toBe(
      false,
    );
  });

  test('shows a dismissible toast when fetching models fails instead of a not-found alert', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi([], {
      previewStatus: 502,
      previewCode: 'upstream_models_unavailable',
      previewMessage: 'wording changed',
    });
    renderPage();

    expect(await screen.findByText('No providers yet')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: 'Add provider' })[0]);
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.change(within(dialog).getByLabelText('Provider name'), { target: { value: 'DashScope' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Fetch model list' }));

    const toastTitle = await screen.findByText('Could not list models from this provider.', {}, { timeout: 2000 });
    const toastElement = toastTitle.closest('[data-sonner-toast]');
    const toaster = toastElement?.closest('[data-sonner-toaster]');
    const toastRegion = toastElement?.closest('section');
    expect(toastElement?.getAttribute('data-type')).toBe('error');
    expect(toastRegion?.parentElement === document.body).toBeTrue();
    expect(toaster?.getAttribute('data-x-position')).toBe('right');
    expect(toaster?.getAttribute('data-y-position')).toBe('bottom');
    expect(screen.getByRole('dialog', { name: 'Add provider' })).toBeTruthy();
    expect(within(dialog).queryByText('not found')).toBeNull();
    expect(screen.queryByText('not found')).toBeNull();
  });

  test('localizes a slug conflict returned while saving a provider', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi([], {
      createStatus: 409,
      createCode: 'llm_provider_slug_conflict',
      createMessage: 'wording changed',
    });
    renderPage('zh-CN');

    expect(await screen.findByText('尚未配置 Provider')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: '添加 Provider' })[0]);
    const dialog = screen.getByRole('dialog', { name: '添加 Provider' });
    fireEvent.change(within(dialog).getByLabelText('Provider 名称'), { target: { value: '阿里云' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API Key'), { target: { value: 'test-secret-1111' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: '模型 ID 1' }), {
      target: { value: 'glm-4.7' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }));

    expect(await within(dialog).findByText('已存在使用该 slug 的 Provider。')).toBeTruthy();
    expect(within(dialog).queryByText('wording changed')).toBeNull();
    expect(api.requests.find((request) => request.method === 'POST')?.body).not.toHaveProperty('slug');
  });
});

async function createProvider(name: string, baseUrl: string, apiKey: string, modelIDs: string[]) {
  fireEvent.click(screen.getAllByRole('button', { name: 'Add provider' })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add provider' });
  fireEvent.change(within(dialog).getByLabelText('Provider name'), { target: { value: name } });
  fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: baseUrl } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: apiKey } });
  fireEvent.change(within(dialog).getByRole('textbox', { name: 'Model ID 1' }), {
    target: { value: modelIDs[0] },
  });
  for (let index = 1; index < modelIDs.length; index += 1) {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add model' }));
    fireEvent.change(within(dialog).getByRole('textbox', { name: `Model ID ${index + 1}` }), {
      target: { value: modelIDs[index] },
    });
  }
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  await screen.findByRole('button', { name }, { timeout: 2000 });
}

const rootRoute = createRootRoute();
const fallbackRoute = createRoute({ getParentRoute: () => rootRoute, path: '$' });
const routeTree = rootRoute.addChildren([fallbackRoute]);

function TestRouter({ children }: { children: ReactNode }) {
  const router = createRouter({ history: createBrowserHistory({ window }), routeTree });
  useEffect(() => {
    const unsubscribe = router.history.subscribe(router.load);
    return () => {
      unsubscribe();
      router.history.destroy();
    };
  }, [router]);
  return <RouterContextProvider router={router}>{children}</RouterContextProvider>;
}

function renderPage(locale: Locale = 'en', role = 'admin') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const authValue: AuthContextValue = {
    account: {
      uuid: 'acct_test',
      email_address: 'test@example.com',
      memberships: [{ role, organization: { uuid: 'org_test' } }],
    },
    status: 'authenticated',
    csrfToken: 'csrf_test',
    refresh: async () => ({ account: null }),
    logout: async () => undefined,
  };
  const workspaceValue: WorkspaceContextValue = {
    orgUuid: 'org_test',
    workspaces: [defaultWorkspace],
    activeWorkspace: defaultWorkspace,
    activeWorkspaceId: defaultWorkspace.id,
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => defaultWorkspace,
    refreshWorkspaces: async () => undefined,
  };
  return render(
    <TestRouter>
      <I18nProvider initialLocale={locale}>
        <AuthContext.Provider value={authValue}>
          <WorkspaceContext.Provider value={workspaceValue}>
            <QueryClientProvider client={queryClient}>
              <Toaster closeButton toastOptions={{ closeButtonAriaLabel: 'Close' }} />
              <LLMModelsPage />
            </QueryClientProvider>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    </TestRouter>,
  );
}

type RecordedRequest = { url: string; method: string; body?: Record<string, unknown> };

function mockProvidersApi(
  liveModelIds: string[] = [],
  options: {
    previewStatus?: number;
    previewCode?: string;
    previewMessage?: string;
    createStatus?: number;
    createCode?: string;
    createMessage?: string;
    updateStatus?: number;
    updateCode?: string;
    updateMessage?: string;
    updateGate?: Promise<void>;
  } = {},
) {
  let providers: LLMProvider[] = [];
  let nextID = 1;
  const requests: RecordedRequest[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const body = parseBody(init?.body);
    requests.push({ url, method, body });

    if (url.endsWith('/preview_models') && method === 'POST') {
      if (options.previewStatus && options.previewStatus >= 400) {
        return jsonResponse(providerError(options.previewCode, options.previewMessage), options.previewStatus);
      }
      return jsonResponse({ model_ids: liveModelIds });
    }
    if (url.includes('/models/sync') && method === 'POST') {
      const providerID = url.match(/\/llm_providers\/([^/?]+)\/models\/sync$/)?.[1];
      const existing = providers.find((provider) => provider.id === providerID);
      if (!existing) return jsonResponse({ error: { message: 'not found' } }, 404);
      const modelIds = [...existing.model_ids];
      for (const modelId of liveModelIds) {
        if (!modelIds.includes(modelId)) modelIds.push(modelId);
      }
      const updated = { ...existing, model_ids: modelIds };
      providers = providers.map((provider) => (provider.id === providerID ? updated : provider));
      return jsonResponse(updated);
    }
    if (url.endsWith('/llm_providers') && method === 'GET') {
      return jsonResponse(providers);
    }
    if (url.endsWith('/llm_providers') && method === 'POST') {
      if (options.createStatus && options.createStatus >= 400) {
        return jsonResponse(providerError(options.createCode, options.createMessage), options.createStatus);
      }
      const provider = providerFromBody(`provider_${nextID++}`, body);
      providers = [...providers, provider];
      return jsonResponse(provider);
    }

    const providerID = url.match(/\/llm_providers\/([^/?]+)$/)?.[1];
    if (providerID && method === 'PUT') {
      const existing = providers.find((provider) => provider.id === providerID);
      if (!existing) return jsonResponse({ error: { message: 'not found' } }, 404);
      await options.updateGate;
      if (options.updateStatus && options.updateStatus >= 400) {
        return jsonResponse(providerError(options.updateCode, options.updateMessage), options.updateStatus);
      }
      const updated = providerFromBody(providerID, body, existing.api_key_last4);
      providers = providers.map((provider) => (provider.id === providerID ? updated : provider));
      return jsonResponse(updated);
    }
    if (providerID && method === 'DELETE') {
      providers = providers.filter((provider) => provider.id !== providerID);
      return new Response(null, { status: 204 });
    }
    return jsonResponse({ error: { message: 'not found' } }, 404);
  }) as unknown as typeof fetch;
  return { requests };
}

function providerFromBody(id: string, body?: Record<string, unknown>, existingLast4 = ''): LLMProvider {
  const apiKey = typeof body?.api_key === 'string' ? body.api_key : '';
  return {
    type: 'llm_provider',
    id,
    name: String(body?.name ?? ''),
    slug: typeof body?.slug === 'string' ? body.slug : 'default',
    api_backend: body?.api_backend === 'chat_completions' ? 'chat_completions' : 'messages',
    base_url: String(body?.base_url ?? ''),
    has_api_key: Boolean(apiKey || existingLast4),
    api_key_last4: apiKey ? apiKey.slice(-4) : existingLast4,
    model_ids: Array.isArray(body?.model_ids) ? body.model_ids.map(String) : [],
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
  };
}

function parseBody(body: BodyInit | null | undefined): Record<string, unknown> | undefined {
  return typeof body === 'string' ? (JSON.parse(body) as Record<string, unknown>) : undefined;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function providerError(code = 'request_failed', message = 'request failed') {
  return { error: 'invalid_request', code, message };
}

function storageText(storage: Storage) {
  return Array.from({ length: storage.length }, (_, index) => storage.getItem(storage.key(index) ?? '') ?? '').join(
    '\n',
  );
}
