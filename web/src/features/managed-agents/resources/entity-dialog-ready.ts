import { budgetValid } from './budget';
import { sessionEditBody } from '../sessions/sessionEdit';
import { managedResourceFieldsValid } from './git-resource';
import { previewSchedule } from './deployment-schedule';
import type { ManagedEntityFormValues, ManagedEntitySection } from '../types';

type DialogReadiness = {
  submitting: boolean;
  loadingOptions: boolean;
  editing?: boolean;
  vaultAcknowledged?: boolean;
};

function sessionCanSubmit(values: ManagedEntityFormValues, options: DialogReadiness) {
  if (options.submitting || !budgetValid(values)) return false;
  if (options.editing) return Object.keys(sessionEditBody(values)).length > 0;
  return (
    !options.loadingOptions &&
    Boolean(values.agentId.trim() && values.environmentId.trim()) &&
    managedResourceFieldsValid(values, false) &&
    (!values.vaultIds.length || options.vaultAcknowledged === true)
  );
}

export function managedEntityDialogCanSubmit(
  section: ManagedEntitySection,
  values: ManagedEntityFormValues,
  options: DialogReadiness,
) {
  if (section === 'sessions') return sessionCanSubmit(values, options);
  if (options.submitting || options.loadingOptions) {
    return false;
  }
  if (section === 'deployments') {
    if (
      !values.name.trim() ||
      !values.agentId.trim() ||
      !values.environmentId.trim() ||
      !values.initialMessage.trim() ||
      !managedResourceFieldsValid(values, Boolean(options.editing)) ||
      !budgetValid(values)
    ) {
      return false;
    }
    if (values.triggerType === 'manual') {
      return true;
    }
    return values.triggerType === 'schedule' && !previewSchedule(values.cronExpression, values.timezone).error;
  }
  return values.name.trim().length > 0;
}
