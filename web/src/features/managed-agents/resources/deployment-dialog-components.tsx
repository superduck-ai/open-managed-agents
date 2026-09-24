import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { ManagedDialogHeader } from './dialog-components';

export function DeploymentDialogHeader({ title }: { title: string }) {
  const { msg } = useI18n();
  return (
    <ManagedDialogHeader
      title={title}
      subtitle={msg(
        'managedAgents.deployments.dialogSubtitle',
        'Deploy an agent with a trigger, environment, and credentials.',
      )}
    />
  );
}

export function DeploymentDialogActions({
  editing,
  submitting,
  canSubmit,
  onCancel,
}: {
  editing: boolean;
  submitting: boolean;
  canSubmit: boolean;
  onCancel: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="mt-6 flex shrink-0 justify-end gap-2 border-t border-border pt-5">
      <Button type="button" variant="outline" disabled={submitting} onClick={onCancel}>
        {msg('common.cancel', 'Cancel')}
      </Button>
      <Button type="submit" disabled={!canSubmit}>
        {submitting
          ? msg('common.saving', 'Saving...')
          : editing
            ? msg('common.saveChanges', 'Save changes')
            : msg('managedAgents.deployments.createLabel', 'Create deployment')}
      </Button>
    </div>
  );
}
