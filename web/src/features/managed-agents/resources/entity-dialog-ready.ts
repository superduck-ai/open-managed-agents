import { areSessionFileResourcesValid } from '../sessions/file-resource-form';
import type { ManagedEntityFormValues, ManagedEntitySection } from '../types';
import { areMemoryAttachesValid } from './memory-attach';

export function managedEntityDialogCanSubmit(
  section: ManagedEntitySection,
  values: ManagedEntityFormValues,
  options: { submitting: boolean; loadingOptions: boolean; needsReferences: boolean },
) {
  if (options.submitting || options.loadingOptions || !areMemoryAttachesValid(values.memoryAttaches)) {
    return false;
  }
  if (section === 'deployments') {
    if (
      !values.name.trim() ||
      !values.agentId.trim() ||
      !values.environmentId.trim() ||
      !values.initialMessage.trim()
    ) {
      return false;
    }
    if (values.triggerType === 'manual') {
      return true;
    }
    return (
      values.triggerType === 'schedule' && values.cronExpression.trim().length > 0 && values.timezone.trim().length > 0
    );
  }
  const referencesReady =
    !options.needsReferences || (values.agentId.trim().length > 0 && values.environmentId.trim().length > 0);
  return (
    referencesReady &&
    (section === 'sessions' ? areSessionFileResourcesValid(values.fileResources) : values.name.trim().length > 0)
  );
}
