import { X } from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { AgentSelectField } from '../agents/AgentSelectField';
import { LockedAgentReferenceField, ManagedSelectField } from '../components/common';
import { BudgetField } from '../resources/budget-field';
import { entityBudgetUsdInput } from '../resources/budget';
import { budgetErrorMessage } from '../resources/budget-errors';
import { ManagedResourceFields } from '../resources/ManagedResourceFields';
import { managedEntityDialogCanSubmit } from '../resources/entity-dialog-ready';
import { initialFormValues } from '../resources/model';
import { useManagedReferenceOptions } from '../resources/useManagedReferenceOptions';
import { ManagedVaultSelectField } from '../resources/vault-select-field';
import type { AgentApiResponse, ManagedEntityFormValues, SessionApiResponse } from '../types';
import { errorMessage } from '../utils';

export function SessionCreateDialog({
  title,
  workspaceId,
  lockedAgent,
  entity,
  onClose,
  onSubmit,
}: {
  title: string;
  workspaceId: string;
  lockedAgent?: AgentApiResponse;
  entity?: SessionApiResponse;
  onClose: () => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [values, setValues] = useState<ManagedEntityFormValues>(() => ({
    ...initialFormValues('sessions', entity),
    ...(lockedAgent ? { agentId: lockedAgent.id } : {}),
  }));
  const references = useManagedReferenceOptions(workspaceId);
  const [acknowledged, setAcknowledged] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const submittingRef = useRef(false);
  useEffect(() => {
    const firstEnvironmentId = references.environments[0]?.id;
    if (firstEnvironmentId)
      setValues((current) => (current.environmentId ? current : { ...current, environmentId: firstEnvironmentId }));
  }, [references.environments]);
  const patch = (changes: Partial<ManagedEntityFormValues>) => setValues((current) => ({ ...current, ...changes }));
  const canSubmit =
    !references.error &&
    values.name.trim().length <= 500 &&
    managedEntityDialogCanSubmit('sessions', values, {
      submitting,
      loadingOptions: references.loading,
      needsReferences: true,
      vaultAcknowledged: acknowledged,
    });
  const close = () => {
    if (!submittingRef.current) onClose();
  };
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || submittingRef.current) return;
    submittingRef.current = true;
    setSubmitting(true);
    setSubmitError(null);
    try {
      await onSubmit(values);
    } catch (error) {
      setSubmitError(errorMessage(error));
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent
        showCloseButton={false}
        className="session-create-dialog flex max-h-[calc(100dvh-2rem)] flex-col gap-0 p-0 sm:max-w-[640px]"
      >
        <form onSubmit={submit} className="flex min-h-0 flex-col" data-testid="session-create-form">
          <DialogHeader className="relative px-6 pb-5 pt-6 sm:px-8 sm:pt-7">
            <DialogTitle className="pr-8 text-xl font-semibold">{title}</DialogTitle>
            <DialogDescription>
              {msg('managedAgents.sessions.setupDescription', 'Set up an instance of your agent in its environment.')}
            </DialogDescription>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="absolute right-5 top-5 text-muted-foreground hover:text-foreground sm:right-6 sm:top-6"
              aria-label={msg('common.close', 'Close')}
              disabled={submitting}
              onClick={close}
            >
              <X aria-hidden />
            </Button>
          </DialogHeader>
          <div className="subtle-scrollbar min-h-0 flex-1 space-y-5 overflow-y-auto px-6 pb-5 sm:px-8">
            <div className="space-y-2">
              <Label htmlFor="session-create-title">
                {msg('managedAgents.sessions.fieldTitle', 'Title')}{' '}
                <span className="font-normal text-muted-foreground">
                  {msg('managedAgents.common.optionalParen', '(optional)')}
                </span>
              </Label>
              <Input
                id="session-create-title"
                autoFocus
                maxLength={500}
                spellCheck={false}
                className="h-10 bg-background"
                value={values.name}
                placeholder={msg('managedAgents.sessions.runNamePlaceholder', 'Name this run')}
                disabled={submitting}
                onChange={(event) => patch({ name: event.target.value })}
              />
            </div>
            <fieldset disabled={submitting} className="min-w-0 space-y-5">
              {lockedAgent ? (
                <LockedAgentReferenceField agent={lockedAgent} variant="managed" />
              ) : (
                <AgentSelectField
                  workspaceId={workspaceId}
                  value={values.agentId}
                  defaultFirst
                  manage
                  onChange={(agentId) => patch({ agentId })}
                />
              )}
              <ManagedSelectField
                label={msg('managedAgents.environments.kindTitle', 'Environment')}
                manageHref={`/workspaces/${encodeURIComponent(workspaceId)}/environments`}
                manageLabel={msg('managedAgents.environments.manage', 'Manage environments')}
                value={values.environmentId}
                options={references.environments}
                placeholder={
                  references.loading
                    ? msg('managedAgents.environments.loading', 'Loading environments...')
                    : msg('managedAgents.quickstart.selectEnvironment', 'Select an environment')
                }
                onChange={(environmentId) => patch({ environmentId })}
              />
              <ManagedVaultSelectField
                label={msg('managedAgents.credentialVaults.title', 'Credential vaults')}
                optional
                presentation="add"
                workspaceId={workspaceId}
                selectedIds={values.vaultIds}
                options={references.vaults}
                manageHref={`/workspaces/${encodeURIComponent(workspaceId)}/vaults`}
                manageLabel={msg('managedAgents.credentialVaults.manage', 'Manage credential vaults')}
                acknowledged={acknowledged}
                onAcknowledgedChange={setAcknowledged}
                onChange={(vaultIds) => patch({ vaultIds })}
              />
              <ManagedResourceFields
                values={values}
                onChange={setValues}
                workspaceId={workspaceId}
                memoryStores={references.memoryStores}
              />
              {!entity || entityBudgetUsdInput(entity) ? <BudgetField values={values} onChange={patch} /> : null}
            </fieldset>
            {references.error ? (
              <Alert variant="destructive">
                <AlertDescription>
                  <p>{references.error}</p>
                  <Button type="button" variant="outline" size="sm" className="mt-2" onClick={references.retry}>
                    {msg('common.retry', 'Retry')}
                  </Button>
                </AlertDescription>
              </Alert>
            ) : null}
            {submitError ? (
              <Alert variant="destructive">
                <AlertDescription>{budgetErrorMessage(submitError, msg)}</AlertDescription>
              </Alert>
            ) : null}
          </div>
          <DialogFooter className="px-6 pb-6 pt-1 sm:px-8 sm:pb-7">
            <Button type="button" variant="outline" onClick={close} disabled={submitting}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={!canSubmit}>
              {submitting
                ? msg('common.saving', 'Saving...')
                : entity
                  ? msg('common.save', 'Save')
                  : msg('managedAgents.sessions.createLabel', 'Create session')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
