import { AgentSelectField } from '../agents/AgentSelectField';
import { type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { FieldGroup } from '../../../shared/ui/field';
import {
  DeploymentTextField,
  DeploymentTextArea,
  DeploymentSelectField,
  DeploymentAddSelectField,
  LockedAgentReferenceField,
} from '../components/common';
import { type AgentApiResponse, type EntityOption, type ManagedEntityFormValues } from '../types';
import { BudgetField } from './budget-field';
import { DeploymentScheduleFields } from './deployment-schedule-fields';
import { ManagedResourceFields } from './ManagedResourceFields';

function DeploymentSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-5 border-b-[0.5px] border-border py-7 first:pt-1 last:border-0 last:pb-1 md:grid-cols-[180px_minmax(0,1fr)] md:gap-8">
      <h3 className="text-sm font-medium">{title}</h3>
      <FieldGroup className="min-w-0">{children}</FieldGroup>
    </section>
  );
}

export function DeploymentFormFields({
  values,
  lockedAgent,
  workspaceId,
  environments,
  vaults,
  memoryStores,
  loadingOptions,
  editing = false,
  onChange,
}: {
  values: ManagedEntityFormValues;
  lockedAgent?: AgentApiResponse;
  workspaceId: string;
  environments: EntityOption[];
  vaults: EntityOption[];
  memoryStores: EntityOption[];
  loadingOptions: boolean;
  editing?: boolean;
  onChange: (patch: Partial<ManagedEntityFormValues>) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <DeploymentSection title={msg('managedAgents.deployments.configuration', 'Configuration')}>
        <DeploymentTextField
          label={msg('common.name', 'Name')}
          value={values.name}
          placeholder={msg('managedAgents.deployments.namePlaceholder', 'Nightly inbox triage')}
          onChange={(name) => onChange({ name })}
          autoFocus
        />
        {lockedAgent ? (
          <LockedAgentReferenceField agent={lockedAgent} variant="deployment" />
        ) : (
          <AgentSelectField
            workspaceId={workspaceId}
            value={values.agentId}
            manage
            onChange={(agentId) => onChange({ agentId })}
          />
        )}
        <DeploymentSelectField
          label={msg('managedAgents.environments.kindTitle', 'Environment')}
          value={values.environmentId}
          placeholder={
            loadingOptions
              ? msg('managedAgents.environments.loading', 'Loading environments...')
              : msg('managedAgents.quickstart.selectEnvironment', 'Select an environment')
          }
          options={environments}
          manageHref={`/workspaces/${workspaceId}/environments`}
          manageLabel={msg('managedAgents.environments.manage', 'Manage environments')}
          onChange={(environmentId) => onChange({ environmentId })}
        />
        <BudgetField values={values} onChange={onChange} />
        <DeploymentTextArea
          label={msg('managedAgents.deployments.initialMessage', 'Initial message')}
          value={values.initialMessage}
          placeholder={msg(
            'managedAgents.deployments.initialMessagePlaceholder',
            "Summarize today's support tickets and post to #digest",
          )}
          helpText={msg('managedAgents.deployments.initialMessageHelp', 'Sent to the agent at the start of every run.')}
          onChange={(initialMessage) => onChange({ initialMessage })}
        />
      </DeploymentSection>
      <DeploymentSection title={msg('managedAgents.deployments.resources', 'Resources')}>
        <DeploymentAddSelectField
          label={msg('managedAgents.credentialVaults.title', 'Credential vaults')}
          optional
          valueLabel={msg('managedAgents.credentialVaults.kind', 'vault')}
          selectedIds={values.vaultIds}
          options={vaults}
          manageHref={`/workspaces/${workspaceId}/vaults`}
          manageLabel={msg('managedAgents.credentialVaults.manage', 'Manage credential vaults')}
          onChange={(vaultIds) => onChange({ vaultIds })}
        />
        <ManagedResourceFields
          values={values}
          workspaceId={workspaceId}
          editing={editing}
          embedded
          memoryStores={memoryStores}
          onChange={(next) => onChange(next)}
        />
      </DeploymentSection>
      <DeploymentSection title={msg('managedAgents.common.trigger', 'Trigger')}>
        <DeploymentScheduleFields values={values} onChange={onChange} />
      </DeploymentSection>
    </>
  );
}
