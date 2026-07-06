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
      </WorkspaceWebhooksHarness>
    );

    expect(screen.getByText('Open Managed Agents')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Webhooks' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add webhook endpoint' })).toBeTruthy();
    expect(screen.getByText('Webhook endpoints receive event notifications when things happen in your workspace.')).toBeTruthy();
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

  test('creates a webhook with default event subscriptions and shows the one-time signing secret', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>
    );

    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getByRole('button', { name: 'Add webhook endpoint' }));

    const dialog = screen.getByRole('dialog', { name: 'Create webhook endpoint' });
    expect(within(dialog).getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(true);
    expect(within(dialog).getAllByText('4 of 4').length).toBe(2);
    expect(within(dialog).getAllByText('3 of 3').length).toBe(2);
    expect(within(dialog).getByText('1 of 1')).toBeTruthy();

    fireEvent.change(within(dialog).getByPlaceholderText('https://example.com/webhooks'), {
      target: { value: 'https://example.com/webhooks' }
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    const createdDialog = await screen.findByRole('dialog', { name: 'Webhook endpoint created' });
    expect(screen.getByText('whsec_local_secret')).toBeTruthy();
    const createdSecretCard = createdDialog.querySelector('[data-slot="card"]') as HTMLElement | null;
    expect(createdSecretCard).toBeTruthy();
    expect(createdSecretCard?.className).toContain('bg-card');
    expect(createdSecretCard?.className).not.toContain('bg-secondary');

    const createRequest = api.requests.find((request) => request.method === 'POST' && request.url === '/v1/webhooks?beta=true');
    expect(createRequest?.body?.url).toBe('https://example.com/webhooks');
    expect(createRequest?.body?.name).toBe('example.com');
    expect((createRequest?.body?.enabled_events as string[]).length).toBe(15);
    expect(createRequest?.headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
  });

  test('updates event group counts as subscriptions are toggled before create', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>
    );

    await screen.findByText('No webhook endpoints have been created for Default.');
    fireEvent.click(screen.getByRole('button', { name: 'Add webhook endpoint' }));
    const dialog = screen.getByRole('dialog', { name: 'Create webhook endpoint' });

    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Session lifecycle events' }));
    expect(within(dialog).getByText('0 of 4')).toBeTruthy();

    fireEvent.change(within(dialog).getByPlaceholderText('https://example.com/webhooks'), {
      target: { value: 'https://example.com/hooks' }
    });
    fireEvent.change(within(dialog).getByPlaceholderText('My webhook endpoint'), {
      target: { value: 'Custom events' }
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    await screen.findByRole('dialog', { name: 'Webhook endpoint created' });
    const createRequest = api.requests.find((request) => request.method === 'POST' && request.url === '/v1/webhooks?beta=true');
    expect(createRequest?.body?.name).toBe('Custom events');
    expect(createRequest?.body?.enabled_events).not.toContain('session.status_run_started');
    expect((createRequest?.body?.enabled_events as string[]).length).toBe(11);
  });

  test('opens row actions and enables disabled endpoints', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([disabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>
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
      </WorkspaceWebhooksHarness>
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
      </WorkspaceWebhooksHarness>
    );

    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Prod events https://example.com/prod' }));

    const inspector = screen.getByRole('dialog', { name: 'Prod events' });
    fireEvent.click(within(inspector).getByRole('button', { name: 'Edit webhook' }));
    fireEvent.change(within(inspector).getByLabelText('Name (optional)'), {
      target: { value: 'Prod deliveries' }
    });
    fireEvent.change(within(inspector).getByLabelText('Description (optional)'), {
      target: { value: 'Production webhook stream' }
    });
    fireEvent.click(within(inspector).getByRole('checkbox', { name: 'Vault lifecycle events' }));
    fireEvent.click(within(inspector).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(api.lastUpdateFor('wh_enabled')?.name).toBe('Prod deliveries'));
    expect(api.lastUpdateFor('wh_enabled')?.description).toBe('Production webhook stream');
    expect(api.lastUpdateFor('wh_enabled')?.enabled_events).toEqual(['vault.created', 'vault.archived', 'vault.deleted']);
    expect(api.lastUpdateFor('wh_enabled')?.status).toBeUndefined();

    const updatedInspector = await screen.findByRole('dialog', { name: 'Prod deliveries' });
    expect(within(updatedInspector).getByText('Vault lifecycle')).toBeTruthy();
    expect(within(updatedInspector).getByText('Created · Archived · Deleted')).toBeTruthy();
    expect(screen.getAllByText('Prod deliveries').length).toBe(2);
  });

  test('regenerates signing secrets and shows the one-time secret', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([enabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>
    );

    await screen.findByText('Prod events');
    fireEvent.click(screen.getByRole('button', { name: 'Webhook actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Regenerate signing secret' }));

    const confirmDialog = screen.getByRole('alertdialog', { name: 'Regenerate signing secret?' });
    expect(
      within(confirmDialog).getByText(
        'This will replace the current signing secret for Prod events. Existing receivers must be updated to verify future deliveries.'
      )
    ).toBeTruthy();
    fireEvent.click(within(confirmDialog).getByRole('button', { name: 'Regenerate' }));

    const regeneratedDialog = await screen.findByRole('dialog', { name: 'Signing secret regenerated' });
    expect(screen.getByText('whsec_regenerated_secret')).toBeTruthy();
    const regeneratedSecretCard = regeneratedDialog.querySelector('[data-slot="card"]') as HTMLElement | null;
    expect(regeneratedSecretCard).toBeTruthy();
    expect(regeneratedSecretCard?.className).toContain('bg-card');
    expect(regeneratedSecretCard?.className).not.toContain('bg-secondary');

    const regenerateRequest = api.requests.find(
      (request) => request.method === 'POST' && request.url === '/v1/webhooks/wh_enabled/regenerate_signing_secret?beta=true'
    );
    expect(regenerateRequest?.body).toEqual({});
    expect(regenerateRequest?.headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(api.regeneratedIds).toContain('wh_enabled');
  });

  test('deletes endpoints through a destructive confirmation dialog', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/webhooks');
    const api = mockWebhooks([enabledWebhook]);

    render(
      <WorkspaceWebhooksHarness>
        <WorkspaceWebhooksContent routeWorkspaceId="default" />
      </WorkspaceWebhooksHarness>
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

function WorkspaceWebhooksHarness({ children }: { children: ReactNode }) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const workspaceValue = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      workspaces: [defaultWorkspace],
      activeWorkspace: defaultWorkspace,
      activeWorkspaceId: defaultWorkspace.id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined
    }),
    []
  );

  setConsoleRequestContext({
    organizationUuid: 'org_test',
    workspaceId: defaultWorkspace.id
  });

  return (
    <QueryClientProvider client={queryClient}>
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

function mockWebhooks(initialWebhooks: WebhookEndpoint[]) {
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
        signing_secret: 'whsec_local_secret'
      };
      webhooks = [created, ...webhooks];
      return jsonResponse(created);
    }

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
          ...(typeof body?.name === 'string' ? { name: body.name } : {}),
          ...(typeof body?.description === 'string' ? { description: body.description } : {}),
          ...(Array.isArray(body?.enabled_events) ? { enabled_events: body.enabled_events as string[] } : {}),
          ...(body?.status === 'enabled' || body?.status === 'disabled'
            ? { status: body.status, disabled_reason: body.status === 'enabled' ? null : 'manual' }
            : {}),
          updated_at: '2026-06-25T09:00:00Z'
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
    }
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
    headers: { 'Content-Type': 'application/json' }
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
  updated_at: '2026-06-25T08:00:00Z'
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
  updated_at: '2026-06-24T08:00:00Z'
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
    'vault_credential.refresh_failed'
  ],
  status: 'enabled',
  disabled_reason: null,
  created_at: '2026-06-25T07:58:00Z',
  updated_at: '2026-06-25T08:00:00Z'
};
