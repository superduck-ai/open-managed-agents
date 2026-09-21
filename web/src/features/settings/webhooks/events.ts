type WebhookEvent = {
  type: string;
  label: string;
};

type WebhookEventGroup = {
  label: string;
  events: WebhookEvent[];
};

type WebhookEventSummaryGroup = {
  label: string;
  labels: string[];
};

export const webhookEventGroups: WebhookEventGroup[] = [
  {
    label: 'Session lifecycle',
    events: [
      { label: 'Run started', type: 'session.status_run_started' },
      { label: 'Rescheduled', type: 'session.status_rescheduled' },
      { label: 'Idled', type: 'session.status_idled' },
      { label: 'Terminated', type: 'session.status_terminated' },
    ],
  },
  {
    label: 'Threads',
    events: [
      { label: 'Created', type: 'session.thread_created' },
      { label: 'Idled', type: 'session.thread_idled' },
      { label: 'Terminated', type: 'session.thread_terminated' },
    ],
  },
  {
    label: 'Outcomes',
    events: [{ label: 'Evaluation ended', type: 'session.outcome_evaluation_ended' }],
  },
  {
    label: 'Session record',
    events: [
      { label: 'Updated', type: 'session.updated' },
      { label: 'Deleted', type: 'session.deleted' },
    ],
  },
  {
    label: 'Vault lifecycle',
    events: [
      { label: 'Created', type: 'vault.created' },
      { label: 'Archived', type: 'vault.archived' },
      { label: 'Deleted', type: 'vault.deleted' },
    ],
  },
  {
    label: 'Credential lifecycle',
    events: [
      { label: 'Created', type: 'vault_credential.created' },
      { label: 'Archived', type: 'vault_credential.archived' },
      { label: 'Deleted', type: 'vault_credential.deleted' },
      { label: 'Refresh failed', type: 'vault_credential.refresh_failed' },
    ],
  },
  {
    label: 'Environment',
    events: [
      { label: 'Created', type: 'environment.created' },
      { label: 'Updated', type: 'environment.updated' },
      { label: 'Archived', type: 'environment.archived' },
      { label: 'Deleted', type: 'environment.deleted' },
    ],
  },
  {
    label: 'Memory Store',
    events: [
      { label: 'Created', type: 'memory_store.created' },
      { label: 'Archived', type: 'memory_store.archived' },
      { label: 'Deleted', type: 'memory_store.deleted' },
    ],
  },
];

export const allWebhookEventTypes = webhookEventGroups.flatMap((group) => group.events.map((event) => event.type));

export function orderedEvents(events: Set<string>) {
  return allWebhookEventTypes.filter((eventType) => events.has(eventType));
}

export function summarizeWebhookEvents(events: string[]): WebhookEventSummaryGroup[] {
  const selected = new Set(events);
  return webhookEventGroups
    .map((group) => ({
      label: group.label,
      labels: group.events.filter((event) => selected.has(event.type)).map((event) => event.label),
    }))
    .filter((group) => group.labels.length > 0);
}
