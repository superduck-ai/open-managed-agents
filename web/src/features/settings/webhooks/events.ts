import type { I18nContextValue } from '../../../shared/i18n/context';

type WebhookEvent = {
  type: string;
  labelId: string;
  label: string;
};

type WebhookEventGroup = {
  labelId: string;
  label: string;
  events: WebhookEvent[];
};

type WebhookEventSummaryGroup = {
  label: string;
  labels: string[];
};

export const webhookEventGroups: WebhookEventGroup[] = [
  {
    labelId: 'webhooks.group.sessionLifecycle',
    label: 'Session lifecycle',
    events: [
      { labelId: 'webhooks.event.runStarted', label: 'Run started', type: 'session.status_run_started' },
      { labelId: 'webhooks.event.rescheduled', label: 'Rescheduled', type: 'session.status_rescheduled' },
      { labelId: 'webhooks.event.idled', label: 'Idled', type: 'session.status_idled' },
      { labelId: 'webhooks.event.terminated', label: 'Terminated', type: 'session.status_terminated' },
    ],
  },
  {
    labelId: 'webhooks.group.threads',
    label: 'Threads',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'session.thread_created' },
      { labelId: 'webhooks.event.idled', label: 'Idled', type: 'session.thread_idled' },
      { labelId: 'webhooks.event.terminated', label: 'Terminated', type: 'session.thread_terminated' },
    ],
  },
  {
    labelId: 'webhooks.group.outcomes',
    label: 'Outcomes',
    events: [
      {
        labelId: 'webhooks.event.evaluationEnded',
        label: 'Evaluation ended',
        type: 'session.outcome_evaluation_ended',
      },
    ],
  },
  {
    labelId: 'webhooks.group.budget',
    label: 'Budget',
    events: [{ labelId: 'webhooks.event.budgetReached', label: 'Budget reached', type: 'session.budget_reached' }],
  },
  {
    labelId: 'webhooks.group.sessionRecord',
    label: 'Session record',
    events: [
      { labelId: 'webhooks.event.updated', label: 'Updated', type: 'session.updated' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'session.deleted' },
    ],
  },
  {
    labelId: 'webhooks.group.vaultLifecycle',
    label: 'Vault lifecycle',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'vault.created' },
      { labelId: 'webhooks.event.archived', label: 'Archived', type: 'vault.archived' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'vault.deleted' },
    ],
  },
  {
    labelId: 'webhooks.group.credentialLifecycle',
    label: 'Credential lifecycle',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'vault_credential.created' },
      { labelId: 'webhooks.event.archived', label: 'Archived', type: 'vault_credential.archived' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'vault_credential.deleted' },
      { labelId: 'webhooks.event.refreshFailed', label: 'Refresh failed', type: 'vault_credential.refresh_failed' },
    ],
  },
  {
    labelId: 'webhooks.group.environment',
    label: 'Environment',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'environment.created' },
      { labelId: 'webhooks.event.updated', label: 'Updated', type: 'environment.updated' },
      { labelId: 'webhooks.event.archived', label: 'Archived', type: 'environment.archived' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'environment.deleted' },
    ],
  },
  {
    labelId: 'webhooks.group.deploymentRun',
    label: 'Deployment run',
    events: [
      { labelId: 'webhooks.event.started', label: 'Started', type: 'deployment_run.started' },
      { labelId: 'webhooks.event.succeeded', label: 'Succeeded', type: 'deployment_run.succeeded' },
      { labelId: 'webhooks.event.failed', label: 'Failed', type: 'deployment_run.failed' },
    ],
  },
  {
    labelId: 'webhooks.group.deployment',
    label: 'Deployment',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'deployment.created' },
      { labelId: 'webhooks.event.updated', label: 'Updated', type: 'deployment.updated' },
      { labelId: 'webhooks.event.paused', label: 'Paused', type: 'deployment.paused' },
      { labelId: 'webhooks.event.unpaused', label: 'Unpaused', type: 'deployment.unpaused' },
      { labelId: 'webhooks.event.archived', label: 'Archived', type: 'deployment.archived' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'deployment.deleted' },
    ],
  },
  {
    labelId: 'webhooks.group.agent',
    label: 'Agent',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'agent.created' },
      { labelId: 'webhooks.event.updated', label: 'Updated', type: 'agent.updated' },
      { labelId: 'webhooks.event.archived', label: 'Archived', type: 'agent.archived' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'agent.deleted' },
    ],
  },
  {
    labelId: 'webhooks.group.memoryStore',
    label: 'Memory Store',
    events: [
      { labelId: 'webhooks.event.created', label: 'Created', type: 'memory_store.created' },
      { labelId: 'webhooks.event.archived', label: 'Archived', type: 'memory_store.archived' },
      { labelId: 'webhooks.event.deleted', label: 'Deleted', type: 'memory_store.deleted' },
    ],
  },
];

export const allWebhookEventTypes = webhookEventGroups.flatMap((group) => group.events.map((event) => event.type));

export function orderedEvents(events: Set<string>) {
  return allWebhookEventTypes.filter((eventType) => events.has(eventType));
}

export function localizedWebhookEventGroups(msg: I18nContextValue['msg']) {
  return webhookEventGroups.map((group) => ({
    ...group,
    label: msg(group.labelId, group.label),
    events: group.events.map((event) => ({ ...event, label: msg(event.labelId, event.label) })),
  }));
}

export function summarizeWebhookEvents(events: string[], msg: I18nContextValue['msg']): WebhookEventSummaryGroup[] {
  const selected = new Set(events);
  return localizedWebhookEventGroups(msg)
    .map((group) => ({
      label: group.label,
      labels: group.events.filter((event) => selected.has(event.type)).map((event) => event.label),
    }))
    .filter((group) => group.labels.length > 0);
}
