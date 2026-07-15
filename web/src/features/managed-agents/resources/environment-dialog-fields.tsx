import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DialogClose, DialogDescription, DialogTitle } from '../../../shared/ui/dialog';
import { X } from 'lucide-react';
import { ManagedTextArea, ManagedTextField } from '../components/common';
import { submitLabel } from '../labels';
import { type ManagedEntitySection } from '../types';
import { EnvironmentDialogCancelControl } from './environment-form';

export function ManagedDialogHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="pr-8">
      <DialogTitle className="text-[22px] font-semibold leading-[26px] text-foreground">{title}</DialogTitle>
      {subtitle ? (
        <DialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">{subtitle}</DialogDescription>
      ) : null}
    </div>
  );
}

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

export function DeploymentDialogCloseControl() {
  const { msg } = useI18n();
  return (
    <DialogClose
      render={
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={msg('common.close', 'Close')}
          className="absolute right-0 top-0 text-foreground hover:bg-accent"
        />
      }
    >
      <X className="size-[22px]" aria-hidden />
    </DialogClose>
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

export function EnvironmentCreationFields({
  description,
  onDescriptionChange,
}: {
  description: string;
  onDescriptionChange: (description: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <ManagedTextField
        label={msg('managedAgents.environments.hostingType', 'Hosting type')}
        value={msg('managedAgents.environments.cloud', 'Cloud')}
        disabled
        onChange={() => undefined}
      />
      <ManagedTextArea
        label={msg('common.description', 'Description')}
        value={description}
        placeholder={msg('managedAgents.common.descriptionPlaceholder', 'Add a description')}
        onChange={onDescriptionChange}
      />
    </>
  );
}

export function ManagedEntityDialogActions({
  section,
  editing,
  submitting,
  canSubmit,
  onRequestClose,
}: {
  section: ManagedEntitySection;
  editing: boolean;
  submitting: boolean;
  canSubmit: boolean;
  onRequestClose: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="mt-5 flex justify-end gap-2">
      <EnvironmentDialogCancelControl
        environment={section === 'environments'}
        submitting={submitting}
        onRequestClose={onRequestClose}
      />
      <Button type="submit" disabled={!canSubmit}>
        {submitting ? msg('common.saving', 'Saving...') : submitLabel(section, editing, msg)}
      </Button>
    </div>
  );
}
