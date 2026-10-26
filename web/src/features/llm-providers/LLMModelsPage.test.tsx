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
  test.each(['context', 'output', 'image', 'whitespace'])(
    'rejects a model row with %s settings and no Model ID',
    async (setting) => {
      resetTestDom('https://oma.duck.ai/settings/llm');
      const api = mockProvidersApi([], { organizationProvider: organizationProvider() });
      renderPage('en', 'admin', 'organization');
      fireEvent.click(await screen.findByRole('button', { name: 'Organization default' }));
      fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
      const dialog = screen.getByRole('dialog', { name: 'Edit provider' });
      fireEvent.change(within(dialog).getByLabelText('Model ID 1'), {
        target: { value: setting === 'whitespace' ? '   ' : '' },
      });
      fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 1' }));
      if (setting === 'image') fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Image' }));
      else
        fireEvent.change(
          within(dialog).getByLabelText(setting === 'output' ? 'Maximum output tokens 1' : 'Context window 1'),
          { target: { value: '32K' } },
        );
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
      expect(within(dialog).getByText('Model ID is required when model parameters are configured.')).toBeTruthy();
      expect(api.requests.filter((request) => request.method === 'PUT')).toHaveLength(0);
    },
  );

  test('allows an untouched empty model placeholder', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    const provider = organizationProvider();
    provider.models = [];
    const api = mockProvidersApi([], { organizationProvider: provider });
    renderPage('en', 'admin', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default' }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add model' }));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(api.requests.findLast((request) => request.method === 'PUT')?.body?.models).toEqual([]);
  });

  test('preserves expanded model parameters when removing an earlier row and editing the model ID', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    const provider = organizationProvider();
    provider.models = [{ id: 'first' }, { id: 'second', context_window: 1000000 }];
    mockProvidersApi([], { organizationProvider: provider });
    renderPage('en', 'admin', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: provider.name, exact: true }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 2' }));
    fireEvent.click(within(dialog).getAllByRole('button', { name: 'Remove model' })[0]!);
    expect(within(dialog).getByLabelText('Model ID 1').getAttribute('value')).toBe('second');
    expect(within(dialog).getByRole('button', { name: 'Model parameters 1' }).getAttribute('aria-expanded')).toBe(
      'true',
    );
    expect(within(dialog).getByLabelText('Context window 1').getAttribute('value')).toBe('1000000');
    fireEvent.change(within(dialog).getByLabelText('Model ID 1'), { target: { value: 'renamed' } });
    expect(within(dialog).getByRole('button', { name: 'Model parameters 1' }).getAttribute('aria-expanded')).toBe(
      'true',
    );
  });

  test('edits and reloads the context window for an individual organization model', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    const api = mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage('en', 'admin', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default' }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    let dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    expect(within(dialog).queryByLabelText('Provider ID')).toBeNull();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 1' }));
    expect(within(dialog).getByRole('checkbox', { name: 'Text' }).getAttribute('aria-checked')).toBe('true');
    expect(within(dialog).getByRole('checkbox', { name: 'Text' }).getAttribute('aria-disabled') === 'true').toBe(true);
    expect(within(dialog).getByRole('checkbox', { name: 'Image' }).getAttribute('aria-checked')).toBe('false');
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Image' }));
    await waitFor(() =>
      expect(within(dialog).getByRole('checkbox', { name: 'Image' }).getAttribute('aria-checked')).toBe('true'),
    );
    fireEvent.change(within(dialog).getByLabelText('Maximum output tokens 1'), { target: { value: '128K' } });
    fireEvent.change(within(dialog).getByLabelText('Context window 1'), { target: { value: '1M' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(api.requests.findLast((request) => request.method === 'PUT')?.body?.models).toEqual([
      { id: 'shared-model', context_window: 1000000, max_output_tokens: 128000, input_modalities: ['text', 'image'] },
    ]);
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 1' }));
    expect((within(dialog).getByLabelText('Context window 1') as HTMLInputElement).value).toBe('1000000');
    expect((within(dialog).getByLabelText('Maximum output tokens 1') as HTMLInputElement).value).toBe('128000');
    expect(within(dialog).getByRole('checkbox', { name: 'Image' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Text' }));
    await waitFor(() =>
      expect(within(dialog).getByRole('checkbox', { name: 'Image' }).getAttribute('aria-disabled') === 'true').toBe(
        true,
      ),
    );
    fireEvent.change(within(dialog).getByLabelText('Context window 1'), { target: { value: '-1' } });
    const writes = api.requests.filter((request) => request.method === 'PUT').length;
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(
      await within(dialog).findByText('Token limits must be positive integers; K and M suffixes are supported.'),
    ).toBeTruthy();
    expect(api.requests.filter((request) => request.method === 'PUT')).toHaveLength(writes);
  });

  test.each(['organization', 'workspace'] as const)(
    'fetches available models using saved %s credentials without saving the draft',
    async (scope) => {
      resetTestDom(
        scope === 'organization'
          ? 'https://oma.duck.ai/settings/llm'
          : 'https://oma.duck.ai/workspaces/default/llm-models',
      );
      const provider = organizationProvider();
      if (scope === 'workspace') {
        provider.scope = undefined;
        provider.id = 'workspace-provider';
        provider.name = 'Workspace gateway';
        provider.slug = 'gateway';
      }
      const api = mockProvidersApi(
        ['shared-model', 'new-model'],
        scope === 'organization' ? { organizationProvider: provider } : { initialProviders: [provider] },
      );
      renderPage('en', 'admin', scope);
      fireEvent.click(await screen.findByRole('button', { name: provider.name, exact: true }));
      fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
      const dialog = screen.getByRole('dialog', { name: 'Edit provider' });
      fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 1' }));
      fireEvent.change(within(dialog).getByLabelText('Context window 1'), { target: { value: '1M' } });
      fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));
      fireEvent.click(await screen.findByRole('button', { name: 'Add selected models' }));
      await waitFor(() =>
        expect(
          within(dialog)
            .getAllByRole('textbox')
            .some((input) => (input as HTMLInputElement).value === 'new-model'),
        ).toBe(true),
      );
      expect((within(dialog).getByLabelText('Context window 1') as HTMLInputElement).value).toBe('1M');
      expect(
        api.requests.some(
          (request) =>
            request.url.endsWith('/models/preview') && request.method === 'POST' && request.body === undefined,
        ),
      ).toBe(true);
      expect(api.requests.some((request) => request.method === 'PUT' || request.url.endsWith('/models/sync'))).toBe(
        false,
      );
      const fetches = api.requests.filter((request) => request.url.endsWith('/models/preview')).length;
      fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://changed.example.com' } });
      fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));
      expect(
        await within(dialog).findByText('Enter an API key to fetch models after changing the endpoint or protocol.'),
      ).toBeTruthy();
      expect(api.requests.filter((request) => request.url.endsWith('/models/preview')).length).toBe(fetches);
      fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
      await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
      fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
      expect(
        within(screen.getByRole('dialog'))
          .getAllByRole('textbox')
          .some((input) => (input as HTMLInputElement).value === 'new-model'),
      ).toBe(false);
    },
  );

  test('keeps Provider ID independent from the display name and requires it on creation', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi();
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    const inputs = within(dialog).getAllByRole('textbox');
    expect(inputs[0]).toBe(within(dialog).getByLabelText('Provider ID'));
    expect(inputs[1]).toBe(within(dialog).getByLabelText('Display name'));
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'Gateway' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://models.example.com' } });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-key' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await within(dialog).findByText('Provider ID is required.')).toBeTruthy();
    expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
    fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'acme-gateway' } });
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'My gateway' } });
    expect((within(dialog).getByLabelText('Provider ID') as HTMLInputElement).value).toBe('acme-gateway');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(api.requests.findLast((request) => request.method === 'POST')?.body).toMatchObject({
      slug: 'acme-gateway',
      name: 'My gateway',
    });
  });

  test('shows capacity hints without saving implicit defaults', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    const api = mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage('en', 'admin', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default', exact: true }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
    let dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 1' }));
    for (const [name, hint] of [
      ['Context window 1', '256K'],
      ['Maximum output tokens 1', '32K'],
    ]) {
      const input = within(dialog).getByLabelText(name!) as HTMLInputElement;
      expect(input.value).toBe('');
      expect(input.placeholder).toBe(hint!);
    }
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    const body = api.requests.findLast((request) => request.method === 'PUT')?.body;
    expect(body?.models).toEqual([{ id: 'shared-model', input_modalities: ['text'] }]);
    fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
    dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Model parameters 1' }));
    expect((within(dialog).getByLabelText('Context window 1') as HTMLInputElement).value).toBe('');
    expect((within(dialog).getByLabelText('Maximum output tokens 1') as HTMLInputElement).value).toBe('');
  });

  test('starts with collapsed directories in both provider views without writing configuration', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage();
    const trigger = await screen.findByRole('button', { name: 'Organization default models' });
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('organization/shared-model')).toBeNull();
    expect(screen.getByText('https://models.example.com')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Organization default' }));
    expect(screen.getByRole('button', { name: 'Organization default models' }).getAttribute('aria-expanded')).toBe(
      'false',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Organization default models' }));
    expect(await screen.findByText('organization/shared-model')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'All providers' }));
    expect(screen.getByRole('button', { name: 'Organization default models' }).getAttribute('aria-expanded')).toBe(
      'true',
    );
    expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
  });

  test('search opens matches temporarily and preserves hidden provider expansion across view changes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi([], {
      initialProviders: [
        providerFromBody('workspace', {
          name: 'Workspace gateway',
          slug: 'workspace',
          base_url: 'https://workspace.example.com',
          api_key: 'test-key',
          models: [{ id: 'local-model' }],
        }),
      ],
      organizationProvider: organizationProvider(),
    });
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Workspace gateway models' }));
    const search = screen.getByLabelText('Search models');
    fireEvent.change(search, { target: { value: 'shared' } });
    expect(screen.getByRole('button', { name: 'Organization default models' }).getAttribute('aria-expanded')).toBe(
      'true',
    );
    expect(screen.getByText('organization/shared-model')).toBeTruthy();
    fireEvent.change(search, { target: { value: '' } });
    expect(screen.getByRole('button', { name: 'Organization default models' }).getAttribute('aria-expanded')).toBe(
      'false',
    );
    expect(screen.getByRole('button', { name: 'Workspace gateway models' }).getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Organization default' }));
    fireEvent.click(screen.getByRole('button', { name: 'Organization default models' }));
    fireEvent.click(screen.getByRole('button', { name: 'Organization default models' }));
    fireEvent.click(screen.getByRole('button', { name: 'All providers' }));
    expect(screen.getByRole('button', { name: 'Workspace gateway models' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('button', { name: 'Organization default models' }).getAttribute('aria-expanded')).toBe(
      'false',
    );
  });

  test('keeps directory expansion after refresh and provider edits without toggling on edit', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    mockProvidersApi(['new-model'], { organizationProvider: organizationProvider() });
    renderPage('en', 'admin', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default' }));
    const trigger = () => screen.getByRole('button', { name: 'Organization default models', hidden: true });
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    const dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(trigger());
    fireEvent.click(screen.getByRole('button', { name: 'Refresh model list' }));
    expect(await screen.findByText('organization/new-model')).toBeTruthy();
    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(trigger().getAttribute('aria-expanded')).toBe('true');
  });

  test('workspace all-provider groups identify inherited organization models as read-only', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage();
    const group = await screen.findByTestId('llm-provider-group-provider_organization');
    expect(
      within(group).getByText('Organization models are read-only here. Manage them in organization LLM configuration.'),
    ).toBeTruthy();
    expect(within(group).queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(within(group).queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  test.each([
    {
      locale: 'en' as const,
      title: 'Add provider',
      name: 'Display name',
      key: 'API key',
      save: 'Save',
      error: 'Provider ID "organization" is reserved for the organization default provider.',
    },
    {
      locale: 'zh-CN' as const,
      title: '添加 Provider',
      name: '显示名称',
      key: 'API 密钥',
      save: '保存',
      error: 'Provider ID“organization”保留给组织默认 Provider。',
    },
  ])(
    'rejects the reserved workspace slug before submitting in $locale',
    async ({ locale, title, name, key, save, error }) => {
      resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
      const api = mockProvidersApi();
      renderPage(locale);
      fireEvent.click(await screen.findByRole('button', { name: title }));
      const dialog = screen.getByRole('dialog', { name: title });
      fireEvent.click(
        within(dialog).getByRole('tab', { name: locale === 'en' ? 'Custom model API' : '自定义模型 API' }),
      );
      fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'organization' } });
      fireEvent.change(within(dialog).getByLabelText(name), { target: { value: 'Organization' } });
      fireEvent.change(within(dialog).getByLabelText(locale === 'en' ? 'Base URL' : 'API 地址'), {
        target: { value: 'https://workspace.example.com' },
      });
      fireEvent.change(within(dialog).getByLabelText(key), { target: { value: 'workspace-key' } });
      fireEvent.click(within(dialog).getByRole('button', { name: save }));
      expect(await within(dialog).findByText(error)).toBeTruthy();
      expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
    },
  );

  test.each([
    {
      locale: 'en' as const,
      remove: 'Delete',
      description:
        'This removes the organization default for every workspace. Workspace providers remain available. Models referenced through this organization provider will stop working.',
    },
    {
      locale: 'zh-CN' as const,
      remove: '删除',
      description:
        '这会移除所有工作区共享的组织默认 Provider。工作区 Provider 仍可使用；通过该组织 Provider 引用的模型将不再可用。',
    },
  ])('explains the organization deletion scope in $locale', async ({ locale, remove, description }) => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage(locale, 'admin', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default' }));
    fireEvent.click(screen.getByRole('button', { name: remove }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(description)).toBeTruthy();
  });

  test('organization members can read defaults without management controls', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    const api = mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage('en', 'user', 'organization');
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default models' }));
    expect(await screen.findByText('organization/shared-model')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Organization default' }));
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Refresh model list' }));
    await waitFor(() => expect(api.requests.length).toBeGreaterThan(1));
    expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
    expect(api.requests.every((request) => !request.url.includes('/workspaces/'))).toBe(true);
  });

  test('workspace administrators view organization defaults without editing them', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    mockProvidersApi([], { organizationProvider: organizationProvider() });
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default models' }));
    expect(await screen.findByText('organization/shared-model')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Organization default' }));
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'new-model' } });
    expect(screen.queryByRole('button', { name: /Add new-model/ })).toBeNull();
  });

  test('creates an organization default using organization preview and persistence endpoints', async () => {
    resetTestDom('https://oma.duck.ai/settings/llm');
    const api = mockProvidersApi(['shared-model']);
    renderPage('en', 'admin', 'organization');
    await screen.findByText('No providers yet');
    fireEvent.click(screen.getByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    const slug = within(dialog).getByLabelText('Provider ID') as HTMLInputElement;
    expect(slug.value).toBe('organization');
    expect(slug.readOnly).toBe(true);
    expect(within(dialog).queryByText('Quick configure AI gateway')).toBeNull();
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'Organization default' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://models.example.com' } });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'organization-key' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith('/llm_provider/preview_models'))).toBe(true),
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Add selected models' }));
    await waitFor(() =>
      expect((within(dialog).getByRole('textbox', { name: 'Model ID 1' }) as HTMLInputElement).value).toBe(
        'shared-model',
      ),
    );
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Organization default models' }));
    expect(await screen.findByText('organization/shared-model')).toBeTruthy();
    expect(api.requests.find((request) => request.method === 'PUT')?.body?.slug).toBe('organization');
    expect(api.requests.every((request) => !request.url.includes('/workspaces/'))).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Organization default' }));
    expect(screen.getByRole('button', { name: 'Edit' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy();
  });

  test('目录读取被拒绝时显示无权限而不是通用失败', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ error: 'workspace not allowed' }), {
          status: 403,
          headers: { 'Content-Type': 'application/json' },
        }),
    ) as unknown as typeof fetch;
    renderPage('zh-CN', 'user');

    expect(await screen.findByText('无权限访问')).toBeTruthy();
    expect(
      screen.getByText('你没有权限查看此工作区的模型。工作区成员可以只读查看目录，只有组织管理员可以管理供应商。'),
    ).toBeTruthy();
    expect(screen.queryByText('请求失败，请重试。')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('button', { name: '添加 Provider' })).toBeNull();
    expect(screen.getByRole('heading', { name: 'LLM 模型' })).toBeTruthy();
  });

  test('shows the English access denied state when the catalog read is forbidden', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ error: 'workspace not allowed' }), {
          status: 403,
          headers: { 'Content-Type': 'application/json' },
        }),
    ) as unknown as typeof fetch;
    renderPage();

    expect(await screen.findByText('Access denied')).toBeTruthy();
    expect(
      screen.getByText(
        'You do not have permission to view models in this workspace. Workspace members can view the catalog read-only, and only organization administrators can manage providers.',
      ),
    ).toBeTruthy();
    expect(screen.queryByText('Request failed. Please try again.')).toBeNull();
  });

  test('目录读取的其他失败仍显示通用错误', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ error: 'unavailable' }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        }),
    ) as unknown as typeof fetch;
    renderPage();

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Request failed. Please try again.')).toBeTruthy();
    expect(screen.queryByText('Access denied')).toBeNull();
  });

  test('用户可以读取模型目录但不能新增供应商', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi();
    renderPage('en', 'user');
    await screen.findByText('No providers yet');
    expect(screen.queryByRole('button', { name: 'Add provider' })).toBeNull();
    expect(api.requests.length).toBeGreaterThan(0);
    expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
  });

  test('用户可筛选全部模型且刷新不会触发同步', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi([], {
      initialProviders: [
        {
          api_backend: 'messages',
          type: 'llm_provider',
          id: 'provider_readonly',
          slug: 'readonly',
          name: 'Read gateway',
          base_url: 'https://llm.example.com',
          has_api_key: true,
          api_key_last4: '1234',
          models: [{ id: 'model-one' }, { id: 'model-two' }],
          created_at: '',
          updated_at: '',
        },
      ],
    });
    renderPage('en', 'user');
    fireEvent.click(await screen.findByRole('button', { name: 'Read gateway models' }));
    await screen.findByText('readonly/model-one');
    expect(screen.getByText('readonly/model-two')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Read gateway' }));
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Add provider' })).toBeNull();
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'new-model' } });
    expect(screen.queryByRole('button', { name: /Add new-model/ })).toBeNull();
    const reads = api.requests.length;
    fireEvent.click(screen.getByRole('button', { name: 'Refresh model list' }));
    await waitFor(() => expect(api.requests.length).toBeGreaterThan(reads));
    expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
  });

  test('creates and edits a Chat Completions provider and discovers using its backend', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const api = mockProvidersApi(['chat-model']);
    renderPage();
    await screen.findByText('No providers yet');
    fireEvent.click(screen.getByRole('button', { name: 'Add provider' }));
    const dialog = screen.getByRole('dialog', { name: 'Add provider' });
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
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
    fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'chat-gateway' } });
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'Chat Gateway' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://chat.example.com' } });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'chat-key' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Add selected models' }));
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
    fireEvent.click(within(createDialog).getByRole('tab', { name: 'Custom model API' }));
    fireEvent.change(within(createDialog).getByLabelText('Provider ID'), { target: { value: 'empty-gateway' } });
    fireEvent.change(within(createDialog).getByLabelText('Display name'), { target: { value: 'Empty Gateway' } });
    fireEvent.change(within(createDialog).getByLabelText('Base URL'), {
      target: { value: 'https://empty.example.com/anthropic' },
    });
    fireEvent.change(within(createDialog).getByLabelText('API key'), { target: { value: 'empty-test-key' } });
    fireEvent.click(within(createDialog).getByRole('button', { name: 'Get available models' }));
    expect(await screen.findByText('The provider returned no models. You can add model IDs later.')).toBeTruthy();
    fireEvent.click(within(createDialog).getByRole('button', { name: 'Save' }));

    fireEvent.click(await screen.findByRole('button', { name: 'Empty Gateway models' }));
    expect(screen.getByText('No models configured for this provider.')).toBeTruthy();
    expect(screen.queryAllByTestId('llm-model-row')).toHaveLength(0);

    fireEvent.click(screen.getByRole('button', { name: 'Empty Gateway' }));
    expect(screen.getByText('No models configured for this provider.')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'glm-4.7' } });
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
    expect(within(selectedDashScope).queryAllByTestId('llm-model-row')).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: 'DashScope models' }));
    expect(within(selectedDashScope).getAllByTestId('llm-model-row')).toHaveLength(2);
    expect(screen.queryByText('test-secret-1111')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editDialog = screen.getByRole('dialog', { name: 'Edit provider' });
    expect(within(editDialog).queryByLabelText('Provider ID')).toBeNull();
    const editApiKey = within(editDialog).getByLabelText('API key') as HTMLInputElement;
    expect(editApiKey.value).toBe('');
    expect(within(editDialog).getByRole('button', { name: 'Get available models' })).toBeTruthy();
    fireEvent.change(editApiKey, { target: { value: 'replacement-key' } });
    expect(within(editDialog).getByRole('button', { name: 'Get available models' })).toBeTruthy();
    fireEvent.change(editApiKey, { target: { value: '' } });
    expect(within(editDialog).getByRole('button', { name: 'Get available models' })).toBeTruthy();
    fireEvent.change(within(editDialog).getByLabelText('Display name'), {
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
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'missing-model-id' } });
    expect(screen.getByText('No models match this search.')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: '' } });

    fireEvent.click(screen.getByRole('button', { name: 'DashScope' }));
    await screen.findByText('dashscope/kimi-k2.5', {}, { timeout: 2000 });

    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'kimi' } });
    expect(screen.getByText('dashscope/kimi-k2.5')).toBeTruthy();
    expect(screen.queryByText('dashscope/qwen-max')).toBeNull();
    expect(screen.getByRole('button', { name: 'Add kimi to DashScope' })).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'glm-4.7' } });
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
    const search = screen.getByLabelText('Search models') as HTMLInputElement;
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
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'dashscope' } });
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'DashScope' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Add selected models' }));

    expect(await within(dialog).findByDisplayValue('glm-4.7', {}, { timeout: 2000 })).toBeTruthy();
    expect(within(dialog).getByDisplayValue('kimi-k2.5')).toBeTruthy();
    expect(api.requests.some((request) => request.url.endsWith('/preview_models'))).toBe(true);

    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    fireEvent.click(await screen.findByRole('button', { name: 'DashScope models' }));
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
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Model ID 1' }), {
      target: { value: 'manual-model' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Add selected models' }));

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
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Model ID 1' }), {
      target: { value: 'manual-model' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));

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
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'glm-4.7' } });
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
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: 'kimi-k2.5' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add kimi-k2.5 to Moonshot' }));

    expect(await screen.findByText('moonshot/kimi-k2.5')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Search models'), { target: { value: '' } });
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
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'DashScope CN' } });
    expect((within(dialog).getByLabelText('Provider ID') as HTMLInputElement).value).toBe('');
    fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'Dash/Scope' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    expect(
      await within(dialog).findByText(
        'Provider ID must contain 1 to 32 lowercase letters, digits, ".", "_" or "-" and start with a letter or digit.',
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
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
    fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'dashscope' } });
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'DashScope' } });
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'test-secret-1111' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Get available models' }));

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
    fireEvent.click(within(dialog).getByRole('tab', { name: '自定义模型 API' }));
    fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: 'aliyun' } });
    fireEvent.change(within(dialog).getByLabelText('显示名称'), { target: { value: '阿里云' } });
    fireEvent.change(within(dialog).getByLabelText('API 地址'), {
      target: { value: 'https://dashscope.example.com/anthropic' },
    });
    fireEvent.change(within(dialog).getByLabelText('API 密钥'), { target: { value: 'test-secret-1111' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: '模型 ID 1' }), {
      target: { value: 'glm-4.7' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }));

    expect(await within(dialog).findByText('该 Provider ID 已存在。')).toBeTruthy();
    expect(within(dialog).queryByText('wording changed')).toBeNull();
    expect(api.requests.find((request) => request.method === 'POST')?.body?.slug).toBe('aliyun');
  });
});

async function createProvider(name: string, baseUrl: string, apiKey: string, modelIDs: string[]) {
  fireEvent.click(screen.getAllByRole('button', { name: 'Add provider' })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add provider' });
  fireEvent.click(within(dialog).getByRole('tab', { name: 'Custom model API' }));
  fireEvent.change(within(dialog).getByLabelText('Provider ID'), { target: { value: name.toLowerCase() } });
  fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: name } });
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
  fireEvent.click(await screen.findByRole('button', { name: `${name} models` }));
}

for (const role of ['admin', 'user'] as const) {
  test(`retries a failed model catalog with GET requests for ${role} without synchronizing providers`, async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    const provider = providerFromBody('provider_retry', {
      name: 'Retry provider',
      slug: 'retry',
      base_url: 'https://retry.example.test',
      models: [{ id: 'example-model' }],
    });
    const methods: string[] = [];
    let catalogAttempts = 0;
    globalThis.fetch = mock(async (input, init) => {
      const url = String(input);
      methods.push(init?.method ?? 'GET');
      if (url.endsWith('/llm_providers')) {
        catalogAttempts++;
        return catalogAttempts === 1
          ? jsonResponse({ error: { message: 'Catalog unavailable' } }, 503)
          : jsonResponse([provider]);
      }
      return jsonResponse({ error: { message: 'Missing organization default' } }, 404);
    }) as unknown as typeof fetch;
    renderPage('en', role);
    await screen.findByRole('alert');
    expect(screen.queryByText('No providers yet')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Retry', exact: true }));
    await screen.findByRole('button', { name: 'Retry provider models', exact: true });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(catalogAttempts).toBe(2);
    expect(methods.every((method) => method === 'GET')).toBe(true);
    if (role === 'user') expect(screen.queryByRole('button', { name: 'Add provider', exact: true })).toBeNull();
  });
}

for (const scope of ['workspace', 'organization'] as const) {
  test(`keeps the ${scope} provider form through successful exit before reloading its owning catalog`, async () => {
    resetTestDom(
      scope === 'organization'
        ? 'https://oma.duck.ai/settings/llm'
        : 'https://oma.duck.ai/workspaces/default/llm-models',
    );
    const provider = providerFromBody('provider_exit', {
      name: 'Exit provider',
      slug: scope === 'organization' ? 'organization' : 'exit',
      base_url: 'https://provider.example.test',
      models: [{ id: 'example-model' }],
    });
    const api = mockProvidersApi(
      [],
      scope === 'organization'
        ? { organizationProvider: { ...provider, scope: 'organization' } }
        : { initialProviders: [provider] },
    );
    const fetchApi = globalThis.fetch;
    let written = false;
    const reloadStates: boolean[] = [];
    globalThis.fetch = mock(async (input, init) => {
      if (init?.method === 'PUT') {
        const response = await fetchApi(input, init);
        written = true;
        return response;
      }
      if (written && (!init?.method || init.method === 'GET')) {
        const dialog = document.querySelector('[data-slot="dialog-content"]');
        reloadStates.push(!dialog || dialog.hasAttribute('data-closed'));
      }
      return fetchApi(input, init);
    }) as unknown as typeof fetch;
    renderPage('en', 'admin', scope);
    fireEvent.click(await screen.findByRole('button', { name: 'Exit provider', exact: true }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
    const dialog = screen.getByRole('dialog', { name: 'Edit provider' });
    const name = within(dialog).getByLabelText('Display name');
    fireEvent.change(name, { target: { value: 'Updated exit provider' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save', exact: true }));
    await waitFor(() => expect(dialog.hasAttribute('data-closed')).toBe(true));
    expect(name).toHaveProperty('value', 'Updated exit provider');
    await screen.findByRole('button', { name: 'Updated exit provider', exact: true });
    expect(reloadStates.length).toBeGreaterThan(0);
    expect(reloadStates.every(Boolean)).toBe(true);
    expect(api.requests.filter((request) => request.method === 'PUT')).toHaveLength(1);
  });
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

function renderPage(locale: Locale = 'en', role = 'admin', scope: 'workspace' | 'organization' = 'workspace') {
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
              <LLMModelsPage scope={scope} />
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
    initialProviders?: LLMProvider[];
    organizationProvider?: LLMProvider;
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
  let providers: LLMProvider[] = options.initialProviders ?? [];
  let organization = options.organizationProvider;
  let nextID = 1;
  const requests: RecordedRequest[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const body = parseBody(init?.body);
    requests.push({ url, method, body });

    if (url.endsWith('/llm_provider_presets') && method === 'GET')
      return jsonResponse({ providers: [], revision: 'test', writable: false });
    if (url.endsWith('/llm_provider') && method === 'GET')
      return organization ? jsonResponse(organization) : jsonResponse({}, 404);
    if (url.endsWith('/llm_provider') && method === 'PUT') {
      organization = { ...providerFromBody('provider_organization', body), scope: 'organization' };
      return jsonResponse(organization);
    }
    if (url.endsWith('/llm_provider') && method === 'DELETE') {
      organization = undefined;
      return new Response(null, { status: 204 });
    }
    if (url.endsWith('/llm_provider/models/sync') && method === 'POST' && organization) {
      organization = { ...organization, models: mergeTestModels(organization.models, liveModelIds) };
      return jsonResponse(organization);
    }
    if ((url.endsWith('/preview_models') || url.endsWith('/models/preview')) && method === 'POST') {
      if (options.previewStatus && options.previewStatus >= 400) {
        return jsonResponse(providerError(options.previewCode, options.previewMessage), options.previewStatus);
      }
      return jsonResponse({ models: liveModelIds.map((id) => ({ id })) });
    }
    if (url.includes('/models/sync') && method === 'POST') {
      const providerID = url.match(/\/llm_providers\/([^/?]+)\/models\/sync$/)?.[1];
      const existing = providers.find((provider) => provider.id === providerID);
      if (!existing) return jsonResponse({ error: { message: 'not found' } }, 404);
      const modelIds = existing.models.map((model) => model.id);
      for (const modelId of liveModelIds) {
        if (!modelIds.includes(modelId)) modelIds.push(modelId);
      }
      const updated = { ...existing, models: mergeTestModels(existing.models, modelIds) };
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
    models: Array.isArray(body?.models) ? (body.models as LLMProvider['models']) : [],
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

function organizationProvider(): LLMProvider {
  return {
    ...providerFromBody('provider_organization', {
      name: 'Organization default',
      slug: 'organization',
      base_url: 'https://models.example.com',
      api_key: 'organization-key',
      models: [{ id: 'shared-model' }],
    }),
    scope: 'organization',
  };
}

function mergeTestModels(existing: LLMProvider['models'], ids: string[]) {
  return [...existing, ...ids.filter((id) => !existing.some((model) => model.id === id)).map((id) => ({ id }))];
}
