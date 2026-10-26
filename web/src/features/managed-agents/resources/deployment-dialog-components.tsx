import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { ManagedDialogHeader } from './dialog-components';

export function DeploymentDialogHeader({ title }: { title: string }) {
  return <ManagedDialogHeader title={title} subtitle="" />;
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
    <div className="mt-6 flex shrink-0 justify-end gap-2 border-t-[0.5px] border-border pt-5">
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
