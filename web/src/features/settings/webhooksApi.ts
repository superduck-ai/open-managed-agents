import { getConsoleRequestContext, webhooksApi } from '../../shared/api/client';

export type WebhookEndpointStatus = 'enabled' | 'disabled';

export type WebhookEndpoint = {
  id: string;
  type: 'webhook';
  url: string;
  name: string;
  description: string;
  enabled_events: string[];
  status: WebhookEndpointStatus;
  disabled_reason?: string | null;
  created_at: string;
  updated_at: string;
};

export type CreatedWebhookEndpoint = WebhookEndpoint & { signing_secret: string };

export type CreateWebhookEndpointInput = {
  url: string;
  name?: string;
  description?: string;
  enabled_events: string[];
};

export type UpdateWebhookEndpointInput = {
  url?: string;
  name?: string;
  description?: string;
  enabled_events?: string[];
  status?: WebhookEndpointStatus;
};

export type RegenerateWebhookSigningSecretResponse = {
  signing_secret: string;
};

type WebhookEndpointPage = {
  data: WebhookEndpoint[];
  next_page?: string | null;
};

export async function listWebhookEndpoints() {
  const page = await webhooksApi<WebhookEndpointPage>('/v1/webhooks?beta=true');
  return page.data;
}

export function createWebhookEndpoint(input: CreateWebhookEndpointInput) {
  return webhooksApi<CreatedWebhookEndpoint>('/v1/webhooks?beta=true', {
    method: 'POST',
    headers: mutationHeaders(),
    body: JSON.stringify(input),
  });
}

export function updateWebhookEndpointStatus(id: string, status: WebhookEndpointStatus) {
  return updateWebhookEndpoint(id, { status });
}

export function updateWebhookEndpoint(id: string, input: UpdateWebhookEndpointInput) {
  return webhooksApi<WebhookEndpoint>(`/v1/webhooks/${encodeURIComponent(id)}?beta=true`, {
    method: 'POST',
    headers: mutationHeaders(),
    body: JSON.stringify(input),
  });
}

export function regenerateWebhookSigningSecret(id: string) {
  return webhooksApi<RegenerateWebhookSigningSecretResponse>(
    `/v1/webhooks/${encodeURIComponent(id)}/regenerate_signing_secret?beta=true`,
    {
      method: 'POST',
      headers: mutationHeaders(),
      body: JSON.stringify({}),
    },
  );
}

export function deleteWebhookEndpoint(id: string) {
  return webhooksApi<{ id: string; type: 'webhook_deleted' }>(`/v1/webhooks/${encodeURIComponent(id)}?beta=true`, {
    method: 'DELETE',
    headers: mutationHeaders(),
  });
}

function mutationHeaders() {
  const { csrfToken } = getConsoleRequestContext();
  return csrfToken ? { 'X-CSRF-Token': csrfToken } : undefined;
}
