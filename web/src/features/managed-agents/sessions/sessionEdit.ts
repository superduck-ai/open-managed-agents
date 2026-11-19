import type { I18nMsg, ManagedEntityFormValues } from '../types';
import { budgetUpdateBody } from '../resources/budget';
import { budgetErrorMessage } from '../resources/budget-errors';
import { errorMessage } from '../utils';

export function sessionEditBody(values: ManagedEntityFormValues) {
  const title = values.name.trim() || null;
  const titleChanged = values.name !== (values.originalSessionTitle ?? '') && title !== values.originalSessionTitle;
  const budget = budgetUpdateBody(values);
  return {
    ...(titleChanged ? { title } : {}),
    ...(budget !== undefined ? { budget } : {}),
  };
}

export function sessionEditErrorMessage(error: unknown, msg: I18nMsg) {
  const detail = errorMessage(error);
  const budgetError = budgetErrorMessage(detail, msg);
  if (budgetError && budgetError !== detail) return budgetError;
  if (detail === 'session must be non-terminated and unarchived to update') {
    return msg('managedAgents.sessions.errors.closed', 'Archived or terminated sessions cannot be edited.');
  }
  if (detail === 'title must contain between 1 and 500 characters') {
    return msg(
      'managedAgents.sessions.errors.titleLength',
      'Use up to 500 characters for the title, or leave it blank.',
    );
  }
  if (detail === 'session must be idle to update the agent') {
    return msg('managedAgents.sessions.errors.agentRequiresIdle', 'Wait until the session is idle to edit the agent.');
  }
  if (detail === 'session must be idle and unarchived to update') {
    return msg('managedAgents.sessions.errors.idleRequired', 'Only idle, unarchived sessions can be updated.');
  }
  if (
    detail === 'agent update must be an object' ||
    detail === 'only tools and mcp_servers can be updated in an existing session'
  ) {
    return msg(
      'managedAgents.sessions.errors.agentUpdate',
      'Only tools and MCP servers can be updated on the session agent.',
    );
  }
  if (error && typeof error === 'object' && 'status' in error) {
    if (error.status === 409)
      return msg('managedAgents.sessions.errors.conflict', 'The session changed. Reopen it and try again.');
    if (error.status === 403)
      return msg('managedAgents.sessions.errors.forbidden', 'You do not have permission to edit this session.');
    if (error.status === 404) return msg('managedAgents.sessions.errors.notFound', 'This session could not be found.');
  }
  return msg('managedAgents.sessions.errors.saveFailed', 'Could not save the session. Please try again.');
}
