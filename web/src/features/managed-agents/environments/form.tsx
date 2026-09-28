import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import type { EnvironmentApiResponse } from '../types';
import {
  environmentErrorMessage,
  hasEnvironmentValidationErrors,
  validateEnvironment,
} from '../resources/environment-model';
import { UnsavedEnvironmentChangesDialog, useUnsavedChangesGuard } from '../resources/environment-form';
import { EnvironmentGeneral, EnvironmentMetadata, EnvironmentNetworking } from './fields';
import { EnvironmentPackages } from './packages';
import { EnvironmentPrebuildStatus } from './prebuild-status';
import { environmentDraftKey, environmentFormBody, environmentFormValues } from './model';

export function EnvironmentForm({
  entity,
  workspaceId,
  onSaved,
  onCancel,
}: {
  entity?: EnvironmentApiResponse;
  workspaceId: string;
  onSaved: (entity: EnvironmentApiResponse) => void;
  onCancel: () => void;
}) {
  const { msg } = useI18n();
  const [initial, setInitial] = useState(() => environmentFormValues(entity));
  const [values, setValues] = useState(initial);
  const [attempted, setAttempted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submitting = useRef(false);
  const syncedEntity = useRef(entity);
  const readOnly = Boolean(entity?.archived_at || entity?.state === 'archived');
  const dirty = environmentDraftKey(values) !== environmentDraftKey(initial);
  useEffect(() => {
    if (dirty || syncedEntity.current === entity) return;
    syncedEntity.current = entity;
    const next = environmentFormValues(entity);
    setInitial(next);
    setValues(next);
  }, [entity, dirty]);
  const reset = () => {
    setValues(initial);
    setAttempted(false);
    setError(null);
  };
  const guard = useUnsavedChangesGuard({ dirty, interactionBlocked: busy, onDiscard: entity ? reset : onCancel });
  const errors = attempted
    ? validateEnvironment(values, msg, entity ? initial : undefined)
    : { packages: {}, metadataRows: {} };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting.current || readOnly) return;
    setAttempted(true);
    if (hasEnvironmentValidationErrors(validateEnvironment(values, msg, entity ? initial : undefined))) return;
    submitting.current = true;
    setBusy(true);
    setError(null);
    try {
      const body = environmentFormBody(values, entity, initial);
      const result = entity
        ? await anthropicBetaApi.environments.update<EnvironmentApiResponse>(entity.id, body, workspaceId)
        : await anthropicBetaApi.environments.create<EnvironmentApiResponse>(body, workspaceId);
      const next = environmentFormValues(result);
      setInitial(next);
      setValues(next);
      setAttempted(false);
      onSaved(result);
    } catch (cause) {
      setError(environmentErrorMessage(cause, entity ? 'update' : 'create', msg));
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };
  const fields = { values, onChange: setValues, errors, readOnly };
  return (
    <>
      <UnsavedEnvironmentChangesDialog
        open={guard.confirmOpen}
        onContinue={guard.continueEditing}
        onDiscard={guard.discard}
      />
      <form className="flex w-full max-w-3xl flex-1 flex-col" onSubmit={submit}>
        <fieldset disabled={busy} className="min-w-0 flex-1 pb-8">
          <EnvironmentGeneral {...fields} creating={!entity} />
          {values.hosting === 'cloud' ? (
            <>
              <EnvironmentNetworking {...fields} />
              <EnvironmentPackages
                {...fields}
                status={
                  <EnvironmentPrebuildStatus
                    entity={entity}
                    workspaceId={workspaceId}
                    packages={values.packages}
                    readOnly={readOnly}
                  />
                }
              />
            </>
          ) : null}
          <EnvironmentMetadata {...fields} />
        </fieldset>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        {!entity ? (
          <div className="sticky bottom-0 z-10 flex shrink-0 justify-end gap-3 border-t border-border bg-background py-3">
            <Button type="button" variant="outline" disabled={busy} onClick={guard.requestDiscard}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={busy}>
              {busy
                ? msg('common.saving', 'Saving...')
                : msg('managedAgents.environments.create', 'Create environment')}
            </Button>
          </div>
        ) : null}
        {entity && dirty && !readOnly ? (
          <div className="fixed bottom-6 left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 rounded-xl border border-border bg-background p-1.5 pl-3 text-sm shadow-lg md:left-[calc(50%+var(--sidebar-width,256px)/2)]">
            <span>{msg('environmentPage.unsaved', 'Unsaved changes')}</span>
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={reset}>
              {msg('common.discard', 'Discard')}
            </Button>
            <Button type="submit" size="sm" disabled={busy}>
              {busy ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
            </Button>
          </div>
        ) : null}
      </form>
    </>
  );
}
