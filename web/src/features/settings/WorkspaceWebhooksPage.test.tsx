import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { useMemo, type ReactNode } from 'react';
import { resetTestDom } from '../../test/setup';
import { ConsoleShell } from '../../app/layout/ConsoleLayout';
import { setConsoleRequestContext } from '../../shared/api/client';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { WorkspaceWebhooksContent } from './WorkspaceWebhooksPage';
import type { WebhookEndpoint } from './webhooksApi';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

describe('Workspace webhooks page', () => {
  test('requires a valid HTTPS URL and an explicit event selection', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([]);
    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent />
      </WorkspaceWebhooksHarness>,
    );
    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]);
    const dialog = screen.getByRole('dialog', { name: 'Create webhook endpoint' });
    const submit = within(dialog).getByRole('button', { name: 'Create' });
    const url = within(dialog).getByLabelText('Endpoint URL');
    fireEvent.change(url, { target: { value: 'https://example.com/hooks' } });
    expect(submit.hasAttribute('disabled')).toBe(true);
    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: 'session.updated' }));
    expect(submit.hasAttribute('disabled')).toBe(false);
    for (const value of [
      'not-a-url',
      'http://example.com',
      'https:example.com',
      'https://example.com:8443',
      'https://user@example.com',
      'https://example.com/#fragment',
    ]) {
      fireEvent.change(url, { target: { value } });
      expect(submit.hasAttribute('disabled')).toBe(true);
      expect(within(dialog).getByRole('alert').textContent).toContain('Must be a valid HTTPS URL');
    }
    expect(api.requests.every((request) => request.method === 'GET')).toBe(true);
  });

  test('keeps action failures open for retry', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    mockWebhooks([enabledWebhook], undefined, 'Permission denied');
    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent />
      </WorkspaceWebhooksHarness>,
    );
    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Webhook actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));
    const dialog = screen.getByRole('alertdialog', { name: 'Delete webhook endpoint' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    expect((await within(dialog).findByRole('alert')).textContent).toContain('Permission denied');
    expect(screen.getByText('Prod events')).toBeTruthy();
  });

  test('clears draft and secret disclosure when the workspace changes', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    mockWebhooks([]);
    const view = render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent />
      </WorkspaceWebhooksHarness>,
    );
    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]);
    fireEvent.change(screen.getByLabelText('Endpoint URL'), { target: { value: 'https://example.com/hooks' } });
    await toggleCheckbox(screen.getByRole('checkbox', { name: 'session.updated' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await screen.findByText('whsec_local_secret');
    view.rerender(
      <WorkspaceWebhooksHarness workspaceId="workspace_other">
        <WorkspaceWebhooksContent />
      </WorkspaceWebhooksHarness>,
    );
    await waitFor(() => expect(screen.queryByText('whsec_local_secret')).toBeNull());
    fireEvent.click(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]);
    expect((screen.getByLabelText('Endpoint URL') as HTMLInputElement).value).toBe('');
    expect(screen.getByText('0 of 27')).toBeTruthy();
  });

  test.each([
    ['Environment', 'environment', 'updated', 'Updated'],
    ['Memory Store', 'memory_store', 'archived', 'Archived'],
    ['Agent', 'agent', 'updated', 'Updated'],
  ])('edits %s events while allowing optional fields to be cleared', async (_group, prefix, change, label) => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([{ ...enabledWebhook, enabled_events: [`${prefix}.created`, `${prefix}.${change}`] }]);
    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent />
      </WorkspaceWebhooksHarness>,
    );
    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Prod events https://example.com/prod' }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit webhook' }));
    const extraEvent = screen.getByRole('checkbox', { name: `${prefix}.${change}` });
    expect(extraEvent.getAttribute('aria-checked')).toBe('true');
    await toggleCheckbox(extraEvent);
    expect(screen.getByRole('checkbox', { name: `${prefix}.${change}` })).toBeTruthy();
    await toggleCheckbox(extraEvent);
    fireEvent.change(screen.getByLabelText('Name (optional)'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(api.lastUpdateFor('wh_enabled')?.name).toBe(''));
    expect(api.lastUpdateFor('wh_enabled')?.enabled_events).toEqual([`${prefix}.created`, `${prefix}.${change}`]);
    expect(await screen.findByText(`Created · ${label}`)).toBeTruthy();
  });

  test('filters IDs and sorts the returned endpoint list', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    mockWebhooks([enabledWebhook, disabledWebhook]);
    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent />
      </WorkspaceWebhooksHarness>,
    );
    await screen.findByText('Prod events');
    expect(screen.getAllByRole('row')[1].textContent).toContain('Deploy events');
    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(screen.getAllByRole('row')[1].textContent).toContain('Prod events');
    fireEvent.change(screen.getByRole('textbox', { name: 'Find webhook by ID' }), { target: { value: 'wh_disabled' } });
    expect(screen.queryByText('Prod events')).toBeNull();
    fireEvent.change(screen.getByRole('textbox', { name: 'Find webhook by ID' }), { target: { value: 'missing' } });
    expect(screen.getByText('No matching webhook IDs.')).toBeTruthy();
  });

  test('renders the official workspace webhooks table in the console shell', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([disabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <ConsoleShell
          currentPath="/settings/workspaces/default/webhooks"
          account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
          onLogout={() => undefined}
        >
          <WorkspaceWebhooksContent routeWorkspaceId="default" />
        </ConsoleShell>
      </WorkspaceWebhooksHarness>,
    );

    expect(
      screen
        .getAllByRole('button', { name: /Default/i })
        .some((button) => button.getAttribute('aria-label') === 'Default'),
    ).toBe(true);
    expect(screen.getByRole('heading', { name: 'Webhooks' })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]).toBeTruthy();
    expect(
      screen.getByText('Webhook endpoints receive event notifications when things happen in your workspace.'),
    ).toBeTruthy();
    expect(screen.getByText('ID')).toBeTruthy();
    expect(screen.getByText('Name')).toBeTruthy();
    expect(screen.getByText('Status')).toBeTruthy();
    expect(screen.getByText('Created at')).toBeTruthy();

    await screen.findByText('Deploy events');
    expect(screen.getByText('https://example.com/webhooks')).toBeTruthy();
    const disabledStatus = screen.getByText('Disabled');
    expect(disabledStatus).toBeTruthy();
    expect(disabledStatus.closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
    expect(screen.getByTestId('workspace-webhooks-page').className).toContain('max-w-none');
    expect(screen.getByTestId('workspace-webhooks-page').parentElement?.className).toContain('lg:px-8');
    expect(api.requests[0].url).toBe('/v1/webhooks?beta=true');
    expect(api.requests[0].headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(api.requests[0].headers.get('x-workspace-id')).toBe('default');
  });

  test('keeps create errors visible outside the scrolling form body', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    mockWebhooks([], 'Webhook rejected');

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]);

    const dialog = screen.getByRole('dialog', { name: 'Create webhook endpoint' });
    fireEvent.change(within(dialog).getByPlaceholderText('https://example.com/webhooks'), {
      target: { value: 'https://example.com/webhooks' },
    });
    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: 'Select all' }));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    const alert = await within(dialog).findByRole('alert');
    expect(alert.textContent).toContain('Webhook rejected');
    expect(dialog.querySelector('.subtle-scrollbar-auto')?.contains(alert)).toBe(false);
  });

  test('creates an unnamed webhook with explicit subscriptions and keeps its one-time secret out of caches', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([]);

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <WorkspaceWebhooksHarness client={client}>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]);

    const dialog = screen.getByRole('dialog', { name: 'Create webhook endpoint' });
    expect(within(dialog).getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(true);
    expect(within(dialog).getAllByText('0 of 4').length).toBe(3);
    expect(within(dialog).getAllByText('0 of 3').length).toBe(4);
    expect(within(dialog).getByText('0 of 1')).toBeTruthy();
    expect(within(dialog).getByText('0 of 27')).toBeTruthy();

    // Layout regression for #122: the header/footer stay pinned while only the form
    // body scrolls, driven by grid rows instead of hardcoded pixel budgets.
    expect(dialog.className).toContain('grid-rows-[auto_minmax(0,1fr)]');
    expect(dialog.className).toContain('overflow-hidden');
    const scrollArea = dialog.querySelector('.subtle-scrollbar-auto') as HTMLElement | null;
    expect(scrollArea?.className).toContain('min-h-0');
    expect(scrollArea?.className).toContain('overflow-y-auto');

    fireEvent.change(within(dialog).getByPlaceholderText('https://example.com/webhooks'), {
      target: { value: 'https://example.com/webhooks' },
    });
    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: 'Select all' }));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    const createdDialog = await screen.findByRole('dialog', { name: 'Webhook endpoint created' });
    expect(screen.getByText('whsec_local_secret')).toBeTruthy();
    const createdSecretCard = createdDialog.querySelector('[data-slot="card"]') as HTMLElement | null;
    expect(createdSecretCard).toBeTruthy();
    expect(createdSecretCard?.className).toContain('bg-card');
    expect(createdSecretCard?.className).not.toContain('bg-secondary');

    const createRequest = api.requests.find(
      (request) => request.method === 'POST' && request.url === '/v1/webhooks?beta=true',
    );
    expect(createRequest?.body?.url).toBe('https://example.com/webhooks');
    expect(createRequest?.body?.name).toBe('');
    expect((createRequest?.body?.enabled_events as string[]).length).toBe(27);
    expect(createRequest?.headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(createRequest?.headers.get('X-CSRF-Token')).toBe('csrf_test');
    const originalClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
    try {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async () => {
            throw new Error('Permission denied');
          },
        },
      });
      fireEvent.click(within(createdDialog).getByRole('button', { name: 'Copy' }));
      expect((await within(createdDialog).findByRole('alert')).textContent).toContain('Could not copy');
      expect(within(createdDialog).queryByRole('button', { name: 'Copied' })).toBeNull();
    } finally {
      if (originalClipboard) Object.defineProperty(navigator, 'clipboard', originalClipboard);
      else Reflect.deleteProperty(navigator, 'clipboard');
    }
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('whsec_');
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((mutation) => mutation.state.data),
      ),
    ).not.toContain('whsec_');
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(screen.queryByText('whsec_local_secret')).toBeNull());
  });

  test.each([
    { group: 'Environment', prefix: 'environment', change: 'updated', count: 4 },
    { group: 'Memory Store', prefix: 'memory_store', change: 'archived', count: 3 },
    { group: 'Agent', prefix: 'agent', change: 'updated', count: 3 },
  ])('updates group counts before create: %j', async ({ group, prefix, change, count }) => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add webhook endpoint' })[0]);
    const dialog = screen.getByRole('dialog', { name: 'Create webhook endpoint' });

    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: `${group} events` }));
    expect(within(dialog).getByText(`${count} of ${count}`)).toBeTruthy();
    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: `${prefix}.${change}` }));
    expect(
      within(dialog)
        .getByRole('checkbox', { name: `${group} events` })
        .getAttribute('aria-checked'),
    ).toBe('mixed');
    expect(within(dialog).getByText(`${count - 1} of 27`)).toBeTruthy();
    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: `${prefix}.${change}` }));
    const selectAll = within(dialog).getByRole('checkbox', { name: 'Select all' });
    expect(selectAll.getAttribute('aria-checked')).toBe('mixed');
    await toggleCheckbox(selectAll);
    expect(within(dialog).getByText('27 of 27')).toBeTruthy();
    await toggleCheckbox(selectAll);
    expect(within(dialog).getByText('0 of 27')).toBeTruthy();
    await toggleCheckbox(within(dialog).getByRole('checkbox', { name: `${group} events` }));

    fireEvent.change(within(dialog).getByPlaceholderText('https://example.com/webhooks'), {
      target: { value: 'https://example.com/hooks' },
    });
    fireEvent.change(within(dialog).getByPlaceholderText('My webhook endpoint'), {
      target: { value: 'Custom events' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    await screen.findByRole('dialog', { name: 'Webhook endpoint created' });
    const createRequest = api.requests.find(
      (request) => request.method === 'POST' && request.url === '/v1/webhooks?beta=true',
    );
    expect(createRequest?.body?.name).toBe('Custom events');
    expect(createRequest?.body?.enabled_events).toContain(`${prefix}.created`);
    expect((createRequest?.body?.enabled_events as string[]).length).toBe(count);
  });

  test('opens row actions and enables disabled endpoints', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([disabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('Deploy events');
    fireEvent.click(screen.getByRole('button', { name: 'Webhook actions' }));

    expect(screen.getByRole('menu', { name: 'Webhook actions' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Enable' })).toBeTruthy();
    const regenerate = screen.getByRole('menuitem', { name: 'Regenerate signing secret' });
    expect(regenerate.getAttribute('data-disabled')).toBeNull();
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeTruthy();

    fireEvent.click(screen.getByRole('menuitem', { name: 'Enable' }));
    expect(screen.getByRole('alertdialog', { name: 'Enable webhook endpoint?' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Enable' }));

    await waitFor(() => expect(api.lastStatusFor('wh_disabled')).toBe('enabled'));
    await waitFor(() => expect(screen.getByText('Enabled')).toBeTruthy());
  });

  test('opens the right-side webhook detail inspector from a row click', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([detailWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('a');
    fireEvent.click(screen.getByRole('button', { name: 'a https://www.baidu.com' }));

    const inspector = screen.getByRole('dialog', { name: 'a' });
    expect(screen.getByTestId('workspace-webhooks-page').className).not.toContain('grid-cols');
    expect(screen.getByTestId('workspace-webhooks-list').className).not.toContain('pr-7');
    expect(screen.getByTestId('webhook-detail-inspector').className).toContain('fixed');
    expect(screen.getByTestId('webhook-detail-inspector').className).toContain('right-0');
    expect(within(inspector).getByText('Endpoint')).toBeTruthy();
    expect(within(inspector).getByText('Events are delivered to this URL via HTTPS POST.')).toBeTruthy();
    const endpointText = within(inspector).getByText('https://www.baidu.com');
    expect(endpointText).toBeTruthy();
    const endpointCard = endpointText.closest('[data-slot="card"]') as HTMLElement | null;
    expect(endpointCard?.getAttribute('data-slot')).toBe('card');
    expect(endpointCard?.className).toContain('bg-card');
    expect(endpointCard?.className).not.toContain('bg-muted');
    expect(within(inspector).getByText('Subscribed events')).toBeTruthy();
    const eventCount = within(inspector).getByText('7');
    expect(eventCount).toBeTruthy();
    expect(eventCount.closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
    expect(within(inspector).getByText('Session lifecycle')).toBeTruthy();
    expect(within(inspector).getByText('Run started · Rescheduled · Idled · Terminated')).toBeTruthy();
    expect(within(inspector).getByText('Session record')).toBeTruthy();
    expect(within(inspector).getByText('Updated · Deleted')).toBeTruthy();
    expect(within(inspector).getByText('Credential lifecycle')).toBeTruthy();
    expect(within(inspector).getByText('Refresh failed')).toBeTruthy();

    fireEvent.click(within(inspector).getByRole('button', { name: 'More actions' }));
    expect(await screen.findByRole('menu', { name: 'More actions' })).toBeTruthy();
    expect(await screen.findByRole('menuitem', { name: 'Disable' })).toBeTruthy();
    expect(api.requests[0].url).toBe('/v1/webhooks?beta=true');
  });

  test('edits webhook details inside the inspector and updates the selected row', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([enabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Prod events https://example.com/prod' }));

    const inspector = screen.getByRole('dialog', { name: 'Prod events' });
    fireEvent.click(within(inspector).getByRole('button', { name: 'Edit webhook' }));
    fireEvent.change(within(inspector).getByLabelText('Name (optional)'), {
      target: { value: 'Prod deliveries' },
    });
    fireEvent.change(within(inspector).getByLabelText('Endpoint URL'), {
      target: { value: 'https://example.com/new-url' },
    });
    fireEvent.change(within(inspector).getByLabelText('Description (optional)'), {
      target: { value: 'Production webhook stream' },
    });
    await toggleCheckbox(within(inspector).getByRole('checkbox', { name: 'Vault lifecycle events' }));
    fireEvent.click(within(inspector).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(api.lastUpdateFor('wh_enabled')?.name).toBe('Prod deliveries'));
    expect(api.lastUpdateFor('wh_enabled')?.url).toBe('https://example.com/new-url');
    expect(api.lastUpdateFor('wh_enabled')?.description).toBe('Production webhook stream');
    expect(api.lastUpdateFor('wh_enabled')?.enabled_events).toEqual([
      'vault.created',
      'vault.archived',
      'vault.deleted',
    ]);
    expect(api.lastUpdateFor('wh_enabled')?.status).toBeUndefined();

    const updatedInspector = await screen.findByRole('dialog', { name: 'Prod deliveries' });
    expect(within(updatedInspector).getByText('Vault lifecycle')).toBeTruthy();
    expect(within(updatedInspector).getByText('Created · Archived · Deleted')).toBeTruthy();
    expect(screen.getAllByText('Prod deliveries').length).toBe(2);
  });

  test('regenerates signing secrets and shows the one-time secret', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([enabledWebhook]);

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <WorkspaceWebhooksHarness client={client}>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Webhook actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Regenerate signing secret' }));

    const confirmDialog = screen.getByRole('alertdialog', { name: 'Regenerate signing secret?' });
    expect(
      within(confirmDialog).getByText(
        'This will replace the current signing secret for Prod events. Existing receivers must be updated to verify future deliveries.',
      ),
    ).toBeTruthy();
    fireEvent.click(within(confirmDialog).getByRole('button', { name: 'Regenerate' }));

    const regeneratedDialog = await screen.findByRole('dialog', { name: 'Signing secret regenerated' });
    expect(screen.getByText('whsec_regenerated_secret')).toBeTruthy();
    const regeneratedSecretCard = regeneratedDialog.querySelector('[data-slot="card"]') as HTMLElement | null;
    expect(regeneratedSecretCard).toBeTruthy();
    expect(regeneratedSecretCard?.className).toContain('bg-card');
    expect(regeneratedSecretCard?.className).not.toContain('bg-secondary');

    const regenerateRequest = api.requests.find(
      (request) =>
        request.method === 'POST' && request.url === '/v1/webhooks/wh_enabled/regenerate_signing_secret?beta=true',
    );
    expect(regenerateRequest?.body).toEqual({});
    expect(regenerateRequest?.headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(api.regeneratedIds).toContain('wh_enabled');
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('whsec_');
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((mutation) => mutation.state.data),
      ),
    ).not.toContain('whsec_');
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(screen.queryByText('whsec_regenerated_secret')).toBeNull());
  });

  test('deletes endpoints through a destructive confirmation dialog', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([enabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>,
    );

    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Webhook actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));

    expect(screen.getByRole('alertdialog', { name: 'Delete webhook endpoint' })).toBeTruthy();
    expect(screen.getByText("Are you sure you want to delete Prod events? This action can't be undone.")).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(api.deletedIds).toContain('wh_enabled'));
    await waitFor(() => expect(screen.queryByText('Prod events')).toBeNull());
  });
});

function WorkspaceWebhooksHarness({
  children,
  client,
  workspaceId = defaultWorkspace.id,
}: {
  children: ReactNode;
  client?: QueryClient;
  workspaceId?: string;
}) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const workspaceValue = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      workspaces: [defaultWorkspace, { ...defaultWorkspace, id: workspaceId }],
      activeWorkspace: { ...defaultWorkspace, id: workspaceId },
      activeWorkspaceId: workspaceId,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    }),
    [workspaceId],
  );

  setConsoleRequestContext({
    organizationUuid: 'org_test',
    workspaceId,
    csrfToken: 'csrf_test',
  });

  return (
    <QueryClientProvider client={client ?? queryClient}>
      <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>
    </QueryClientProvider>
  );
}

type RecordedRequest = {
  url: string;
  method: string;
  headers: Headers;
  body?: Record<string, unknown>;
};

function mockWebhooks(initialWebhooks: WebhookEndpoint[], createError?: string, actionError?: string) {
  let webhooks = [...initialWebhooks];
  const requests: RecordedRequest[] = [];
  const deletedIds: string[] = [];
  const regeneratedIds: string[] = [];

  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const body = parseBody(init?.body);
    const headers = new Headers(init?.headers);
    requests.push({ url, method, headers, body });

    if (url === '/v1/webhooks?beta=true' && method === 'GET') {
      return jsonResponse({ data: webhooks });
    }

    if (url === '/v1/webhooks?beta=true' && method === 'POST') {
      if (createError) {
        return jsonResponse({ error: { message: createError } }, 500);
      }
      const created: WebhookEndpoint = {
        id: 'wh_created',
        type: 'webhook',
        url: typeof body?.url === 'string' ? body.url : 'https://example.com/webhooks',
        name: typeof body?.name === 'string' ? body.name : 'Webhook',
        description: typeof body?.description === 'string' ? body.description : '',
        enabled_events: Array.isArray(body?.enabled_events) ? (body.enabled_events as string[]) : [],
        status: 'enabled',
        disabled_reason: null,
        created_at: '2026-06-25T00:00:00Z',
        updated_at: '2026-06-25T00:00:00Z',
      };
      webhooks = [created, ...webhooks];
      return jsonResponse({ ...created, signing_secret: 'whsec_local_secret' });
    }

    if (method !== 'GET' && actionError) return jsonResponse({ error: { message: actionError } }, 500);

    const regenerateMatch = url.match(/^\/v1\/webhooks\/([^/?]+)\/regenerate_signing_secret\?beta=true$/);
    if (regenerateMatch && method === 'POST') {
      regeneratedIds.push(regenerateMatch[1]);
      return jsonResponse({ signing_secret: 'whsec_regenerated_secret' });
    }

    const webhookId = url.match(/^\/v1\/webhooks\/([^/?]+)\?beta=true$/)?.[1];
    if (webhookId && method === 'POST') {
      webhooks = webhooks.map((webhook) => {
        if (webhook.id !== webhookId) {
          return webhook;
        }
        return {
          ...webhook,
          ...(typeof body?.url === 'string' ? { url: body.url } : {}),
          ...(typeof body?.name === 'string' ? { name: body.name } : {}),
          ...(typeof body?.description === 'string' ? { description: body.description } : {}),
          ...(Array.isArray(body?.enabled_events) ? { enabled_events: body.enabled_events as string[] } : {}),
          ...(body?.status === 'enabled' || body?.status === 'disabled'
            ? { status: body.status, disabled_reason: body.status === 'enabled' ? null : 'manual' }
            : {}),
          updated_at: '2026-06-25T09:00:00Z',
        };
      });
      return jsonResponse(webhooks.find((webhook) => webhook.id === webhookId) ?? { ...enabledWebhook, id: webhookId });
    }

    if (webhookId && method === 'DELETE') {
      deletedIds.push(webhookId);
      webhooks = webhooks.filter((webhook) => webhook.id !== webhookId);
      return jsonResponse({ id: webhookId, type: 'webhook_deleted' });
    }

    return jsonResponse({ error: { message: 'not found' } }, 404);
  }) as unknown as typeof fetch;

  return {
    requests,
    deletedIds,
    regeneratedIds,
    lastStatusFor: (webhookId: string) => {
      const matching = requests
        .filter((request) => request.url === `/v1/webhooks/${webhookId}?beta=true` && request.method === 'POST')
        .at(-1);
      return matching?.body?.status;
    },
    lastUpdateFor: (webhookId: string) => {
      const matching = requests
        .filter((request) => request.url === `/v1/webhooks/${webhookId}?beta=true` && request.method === 'POST')
        .at(-1);
      return matching?.body;
    },
  };
}

function parseBody(body: BodyInit | null | undefined) {
  if (!body || typeof body !== 'string') {
    return undefined;
  }
  return JSON.parse(body) as Record<string, unknown>;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const disabledWebhook: WebhookEndpoint = {
  id: 'wh_disabled',
  type: 'webhook',
  url: 'https://example.com/webhooks',
  name: 'Deploy events',
  description: '',
  enabled_events: ['session.status_run_started'],
  status: 'disabled',
  disabled_reason: 'manual',
  created_at: '2026-06-25T07:58:00Z',
  updated_at: '2026-06-25T08:00:00Z',
};

const enabledWebhook: WebhookEndpoint = {
  id: 'wh_enabled',
  type: 'webhook',
  url: 'https://example.com/prod',
  name: 'Prod events',
  description: '',
  enabled_events: ['vault.created'],
  status: 'enabled',
  disabled_reason: null,
  created_at: '2026-06-24T07:58:00Z',
  updated_at: '2026-06-24T08:00:00Z',
};

const detailWebhook: WebhookEndpoint = {
  id: 'wh_cAqb8DTWDYunTzpoX',
  type: 'webhook',
  url: 'https://www.baidu.com',
  name: 'a',
  description: '',
  enabled_events: [
    'session.status_run_started',
    'session.status_rescheduled',
    'session.status_idled',
    'session.status_terminated',
    'session.updated',
    'session.deleted',
    'vault_credential.refresh_failed',
  ],
  status: 'enabled',
  disabled_reason: null,
  created_at: '2026-06-25T07:58:00Z',
  updated_at: '2026-06-25T08:00:00Z',
};

async function toggleCheckbox(checkbox: HTMLElement) {
  const previous = checkbox.getAttribute('aria-checked');
  fireEvent.click(checkbox);
  await waitFor(() => expect(checkbox.getAttribute('aria-checked')).not.toBe(previous));
}
