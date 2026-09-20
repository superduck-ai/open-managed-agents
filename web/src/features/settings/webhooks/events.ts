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
];

export const allWebhookEventTypes = webhookEventGroups.flatMap((group) => group.events.map((event) => event.type));

const webhookDetailEventGroups: WebhookEventGroup[] = [
  ...webhookEventGroups,
  {
    label: 'Session record',
    events: [
      { label: 'Updated', type: 'session.record_updated' },
      { label: 'Deleted', type: 'session.record_deleted' },
    ],
  },
];

const knownDetailEventTypes = new Set(
  webhookDetailEventGroups.flatMap((group) => group.events.map((event) => event.type)),
);

export function orderedEvents(events: Set<string>) {
  const orderedKnownEvents = allWebhookEventTypes.filter((eventType) => events.has(eventType));
  const extraEvents = Array.from(events)
    .filter((eventType) => !allWebhookEventTypes.includes(eventType))
    .sort((left, right) => left.localeCompare(right));
  return [...orderedKnownEvents, ...extraEvents];
}

export function summarizeWebhookEvents(events: string[]): WebhookEventSummaryGroup[] {
  const selected = new Set(events);
  const consumed = new Set<string>();
  const groups: WebhookEventSummaryGroup[] = [];

  webhookDetailEventGroups.forEach((group) => {
    const labels: string[] = [];
    group.events.forEach((event) => {
      if (!selected.has(event.type)) {
        return;
      }
      consumed.add(event.type);
      if (!labels.includes(event.label)) {
        labels.push(event.label);
      }
    });
    if (labels.length > 0) {
      const existing = groups.find((item) => item.label === group.label);
      if (existing) existing.labels = [...new Set([...existing.labels, ...labels])];
      else groups.push({ label: group.label, labels });
    }
  });

  const unknownLabels = events
    .filter((eventType) => !consumed.has(eventType) && !knownDetailEventTypes.has(eventType))
    .map(prettyWebhookEventType);
  if (unknownLabels.length > 0) {
    groups.push({ label: 'Other', labels: unknownLabels });
  }

  return groups;
}

function prettyWebhookEventType(eventType: string) {
  return eventType
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}
