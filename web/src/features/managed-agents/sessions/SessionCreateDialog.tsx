import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { AgentSelectField } from '../agents/AgentSelectField';
import { ManagedSelectField, ManagedTextField } from '../components/common';
import { ManagedDialogControlledCloseControl } from '../resources/dialog-components';
import { ManagedResourceFields } from '../resources/ManagedResourceFields';
import { ManagedVaultSelectField } from '../resources/vault-select-field';
import type { AgentApiResponse, EntityOption, ManagedEntityFormValues } from '../types';
import type { FormEvent } from 'react';

export function SessionCreateDialog({
  title,
  lockedAgent,
  workspaceId,
  values,
  environments,
  vaults,
  memoryStores,
  loadingOptions,
  vaultAcknowledged,
  submitting,
  canSubmit,
  error,
  onChange,
  onAcknowledgedChange,
  onClose,
  onSubmit,
}: {
  title: string;
  lockedAgent?: AgentApiResponse;
  workspaceId: string;
  values: ManagedEntityFormValues;
  environments: EntityOption[];
  vaults: EntityOption[];
  memoryStores: EntityOption[];
  loadingOptions: boolean;
  vaultAcknowledged: boolean;
  submitting: boolean;
  canSubmit: boolean;
  error: string | null;
  onChange: (values: ManagedEntityFormValues) => void;
  onAcknowledgedChange: (acknowledged: boolean) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const { msg } = useI18n();
  const patch = (value: Partial<ManagedEntityFormValues>) => onChange({ ...values, ...value });
  return (
    <Dialog open onOpenChange={(open) => !open && !submitting && onClose()}>
      <DialogContent
        showCloseButton={false}
        className="flex max-h-[calc(100dvh-2rem)] flex-col rounded-xl bg-popover p-5 sm:min-h-[min(600px,calc(100dvh-2rem))] sm:max-w-[896px] sm:p-7"
      >
        <form onSubmit={onSubmit} className="relative flex min-h-0 flex-1 flex-col" aria-busy={submitting}>
          <ManagedDialogControlledCloseControl disabled={submitting} onRequestClose={onClose} />
          <DialogHeader className="gap-2 pr-9">
            <DialogTitle className="text-2xl font-semibold leading-7">{title}</DialogTitle>
            <DialogDescription className="text-sm">
              {msg('managedAgents.sessions.setupDescription', 'Set up an instance of your agent in its environment.')}
            </DialogDescription>
            {lockedAgent ? <span className="sr-only">{lockedAgent.name || lockedAgent.id}</span> : null}
          </DialogHeader>
          <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-5 overflow-y-auto px-px pb-1 [&_.managed-resource-field]:h-10">
            <ManagedTextField
              label={msg('managedAgents.sessions.fieldTitle', 'Title')}
              value={values.name}
              placeholder={msg('managedAgents.sessions.runNamePlaceholder', 'Name this run')}
              optional
              onChange={(name) => patch({ name })}
              autoFocus
            />
            {!lockedAgent ? (
              <AgentSelectField
                workspaceId={workspaceId}
                value={values.agentId}
                defaultFirst
                onChange={(agentId) => patch({ agentId })}
              />
            ) : null}
            <ManagedSelectField
              label={msg('managedAgents.environments.kindTitle', 'Environment')}
              manageHref={`/workspaces/${encodeURIComponent(workspaceId)}/environments`}
              manageLabel={msg('managedAgents.environments.manage', 'Manage environments')}
              value={values.environmentId}
              placeholder={
                loadingOptions
                  ? msg('managedAgents.environments.loading', 'Loading environments...')
                  : msg('managedAgents.quickstart.selectEnvironment', 'Select an environment')
              }
              options={environments}
              onChange={(environmentId) => patch({ environmentId })}
            />
            <ManagedVaultSelectField
              label={msg('managedAgents.credentialVaults.title', 'Credential vaults')}
              optional
              presentation="add"
              workspaceId={workspaceId}
              selectedIds={values.vaultIds}
              options={vaults}
              manageHref={`/workspaces/${encodeURIComponent(workspaceId)}/vaults`}
              manageLabel={msg('managedAgents.credentialVaults.manage', 'Manage credential vaults')}
              acknowledged={vaultAcknowledged}
              onAcknowledgedChange={onAcknowledgedChange}
              onChange={(vaultIds) => patch({ vaultIds })}
            />
            <ManagedResourceFields
              values={values}
              onChange={onChange}
              workspaceId={workspaceId}
              memoryStores={memoryStores}
              addButtonVariant="secondary"
            />
          </div>
          {error ? (
            <p role="alert" className="mt-4 text-sm text-destructive">
              {error}
            </p>
          ) : null}
          <DialogFooter className="mt-10 flex-row justify-end sm:mt-12">
            <Button type="submit" size="lg" disabled={!canSubmit}>
              {submitting
                ? msg('common.saving', 'Saving...')
                : msg('managedAgents.sessions.createLabel', 'Create session')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
