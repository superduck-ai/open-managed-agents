import { managedResourceFieldsValid } from './git-resource';
import { previewSchedule } from './deployment-schedule';
import type { ManagedEntityFormValues, ManagedEntitySection } from '../types';

export function managedEntityDialogCanSubmit(
  section: ManagedEntitySection,
  values: ManagedEntityFormValues,
  options: {
    submitting: boolean;
    loadingOptions: boolean;
    needsReferences: boolean;
    editing?: boolean;
    vaultAcknowledged?: boolean;
  },
) {
  if (options.submitting || options.loadingOptions) {
    return false;
  }
  if (section === 'deployments') {
    if (
      !values.name.trim() ||
      !values.agentId.trim() ||
      !values.environmentId.trim() ||
      !values.initialMessage.trim() ||
      !managedResourceFieldsValid(values, Boolean(options.editing))
    ) {
      return false;
    }
    if (values.triggerType === 'manual') {
      return true;
    }
    return values.triggerType === 'schedule' && !previewSchedule(values.cronExpression, values.timezone).error;
  }
  const referencesReady =
    !options.needsReferences || (values.agentId.trim().length > 0 && values.environmentId.trim().length > 0);
  return (
    referencesReady &&
    (section === 'sessions'
      ? managedResourceFieldsValid(values, false) && (!values.vaultIds.length || options.vaultAcknowledged === true)
      : values.name.trim().length > 0)
  );
}
