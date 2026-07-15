import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { submitLabel } from '../labels';
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
}: {
  editing: boolean;
  submitting: boolean;
  canSubmit: boolean;
}) {
  const { msg } = useI18n();
  return (
    <div className="mt-5 flex justify-end">
      <Button type="submit" disabled={!canSubmit}>
        {submitting ? msg('common.saving', 'Saving...') : submitLabel('deployments', editing, msg)}
      </Button>
    </div>
  );
}
