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
import { DeploymentScheduleFields } from './deployment-schedule-fields';

function DeploymentSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="grid gap-5 border-b border-border py-7 first:pt-1 last:border-0 last:pb-1 md:grid-cols-[210px_minmax(0,1fr)] md:gap-10">
      <div>
        <h3 className="text-sm font-medium">{title}</h3>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
      <FieldGroup className="min-w-0">{children}</FieldGroup>
    </section>
  );
}

export function DeploymentFormFields({
  values,
  lockedAgent,
  workspaceId,
  agents,
  environments,
  vaults,
  memoryStores,
  loadingOptions,
  onChange,
}: {
  values: ManagedEntityFormValues;
  lockedAgent?: AgentApiResponse;
  workspaceId: string;
  agents: EntityOption[];
  environments: EntityOption[];
  vaults: EntityOption[];
  memoryStores: EntityOption[];
  loadingOptions: boolean;
  onChange: (patch: Partial<ManagedEntityFormValues>) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <DeploymentSection
        title={msg('managedAgents.deployments.configuration', 'Configuration')}
        description={msg(
          'managedAgents.deployments.configurationHelp',
          'Choose the agent, environment, and instructions for every run.',
        )}
      >
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
          <DeploymentSelectField
            label={msg('managedAgents.common.agent', 'Agent')}
            value={values.agentId}
            placeholder={
              loadingOptions
                ? msg('managedAgents.agents.loading', 'Loading agents...')
                : msg('managedAgents.deployments.selectAgent', 'Select an agent')
            }
            options={agents}
            manageHref={`/workspaces/${workspaceId}/agents`}
            manageLabel={msg('managedAgents.agents.manage', 'Manage agents')}
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
      <DeploymentSection
        title={msg('managedAgents.deployments.resources', 'Resources')}
        description={msg(
          'managedAgents.deployments.resourcesHelp',
          'Give the agent access to credentials and persistent memory.',
        )}
      >
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
        <DeploymentAddSelectField
          label={msg('managedAgents.memoryStores.title', 'Memory stores')}
          optional
          valueLabel={msg('managedAgents.memoryStores.kind', 'memory store')}
          selectedIds={values.memoryStoreIds}
          options={memoryStores}
          manageHref={`/workspaces/${workspaceId}/memory-stores`}
          manageLabel={msg('managedAgents.memoryStores.manage', 'Manage memory stores')}
          onChange={(memoryStoreIds) => onChange({ memoryStoreIds })}
        />
      </DeploymentSection>
      <DeploymentSection
        title={msg('managedAgents.common.trigger', 'Trigger')}
        description={msg(
          'managedAgents.deployments.triggerHelp',
          'When a run starts: on demand, or automatically on a schedule.',
        )}
      >
        <DeploymentScheduleFields values={values} onChange={onChange} />
      </DeploymentSection>
    </>
  );
}
