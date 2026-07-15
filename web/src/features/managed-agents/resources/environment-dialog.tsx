import { type FormEvent, useMemo, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Dialog, DialogContent } from '../../../shared/ui/dialog';
import { ManagedTextField } from '../components/common';
import { entityDialogSubtitle } from '../labels';
import { type ManagedEntityApiResponse, type ManagedEntityFormValues } from '../types';
import { ManagedDialogControlledCloseControl, ManagedDialogHeader } from './dialog-components';
import { EnvironmentCreationFields } from './environment-dialog-fields';
import {
  EnvironmentDialogActions,
  UnsavedEnvironmentChangesDialog,
  useEnvironmentDialogDiscardGuard,
} from './environment-form';
import { environmentErrorMessage } from './environment-model';
import { initialFormValues } from './model';

export function EnvironmentEntityDialog({
  title,
  entity,
  onClose,
  onSubmit,
}: {
  title: string;
  entity?: ManagedEntityApiResponse;
  onClose: () => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
}) {
  const { msg } = useI18n();
  const initialValues = useMemo(() => initialFormValues('environments', entity), [entity]);
  const [values, setValues] = useState(initialValues);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const canSubmit = values.name.trim().length > 0 && !submitting;
  const discardGuard = useEnvironmentDialogDiscardGuard({ values, initialValues, submitting, onClose });

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || submittingRef.current) {
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    setSubmitError(null);
    try {
      await onSubmit(values);
    } catch (error) {
      setSubmitError(environmentErrorMessage(error, entity ? 'update' : 'create', msg));
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  return (
    <>
      <UnsavedEnvironmentChangesDialog
        open={discardGuard.confirmOpen}
        onContinue={discardGuard.continueEditing}
        onDiscard={discardGuard.discard}
      />
      <Dialog open onOpenChange={(open) => !open && discardGuard.requestDiscard()}>
        <DialogContent
          className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col sm:max-w-[560px]"
          showCloseButton={false}
        >
          <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
            <ManagedDialogControlledCloseControl disabled={submitting} onRequestClose={discardGuard.requestDiscard} />
            <ManagedDialogHeader title={title} subtitle={entityDialogSubtitle('environments', msg)} />
            <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
              <ManagedTextField
                label={msg('common.name', 'Name')}
                value={values.name}
                placeholder={msg('managedAgents.common.namePlaceholder', 'Enter a name')}
                onChange={(name) => setValues((current) => ({ ...current, name }))}
                autoFocus
              />
              <EnvironmentCreationFields
                description={values.description}
                onDescriptionChange={(description) => setValues((current) => ({ ...current, description }))}
              />
            </div>
            {submitError ? <p className="mt-4 text-sm text-destructive">{submitError}</p> : null}
            <EnvironmentDialogActions
              editing={Boolean(entity)}
              submitting={submitting}
              canSubmit={canSubmit}
              onRequestClose={discardGuard.requestDiscard}
            />
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
