import { useI18n } from '../../../shared/i18n';
import { ManagedDialogHeader, ManagedDialogSubmitButton } from './dialog-components';

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
  return (
    <div className="mt-5 flex justify-end">
      <ManagedDialogSubmitButton
        section="deployments"
        editing={editing}
        submitting={submitting}
        canSubmit={canSubmit}
      />
    </div>
  );
}
