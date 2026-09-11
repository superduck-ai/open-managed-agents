import { ChevronDown, FileText, GitBranch, Plus } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { DeploymentAddSelectField } from '../components/common';
import type { EntityOption, ManagedEntityFormValues } from '../types';
import { SessionFileResourcesField, areSessionFileResourcesValid } from '../sessions/SessionFileResourcesField';
import { GitRepositoryFields } from './GitRepositoryFields';
import { emptyGitResource, gitResourceValid, resourceFormValues } from './git-resource';

export function managedResourceFieldsValid(values: ManagedEntityFormValues, editing: boolean) {
  return (
    (editing && !values.resourcesChanged) ||
    (areSessionFileResourcesValid(values.fileResources) && values.gitResources.every(gitResourceValid))
  );
}

export function ManagedResourceFields({
  values,
  onChange,
  workspaceId,
  editing = false,
  memoryStores,
}: {
  values: ManagedEntityFormValues;
  onChange: (values: ManagedEntityFormValues) => void;
  workspaceId: string;
  editing?: boolean;
  memoryStores?: EntityOption[];
}) {
  const { msg } = useI18n();
  const patch = (value: Partial<ManagedEntityFormValues>) => onChange({ ...values, ...value, resourcesChanged: true });
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-semibold">{msg('managedAgents.sessions.resources.title', 'Resources')}</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          {msg('managedAgents.git.resourcesHelp', 'Mount files and Git repositories into the session.')}
        </p>
      </div>
      {editing && !values.resourcesChanged ? (
        <>
          <ul className="space-y-1 text-sm text-muted-foreground">
            {values.originalResources.map((resource, index) => (
              <li key={index} className="break-all">
                {String(
                  resource.url ?? resource.mount_path ?? resource.memory_store_id ?? resource.file_id ?? resource.type,
                )}
              </li>
            ))}
          </ul>
          <Button type="button" variant="outline" size="sm" onClick={() => patch({})}>
            {msg('managedAgents.git.replaceResources', 'Replace resources')}
          </Button>
        </>
      ) : (
        <>
          {editing ? (
            <div className="space-y-2 text-sm text-muted-foreground">
              <p>
                {msg(
                  'managedAgents.git.replaceHelp',
                  'This replaces the entire resource list. Re-enter tokens for private repositories; leave blank for anonymous access.',
                )}
              </p>
              <Button
                type="button"
                variant="link"
                size="sm"
                onClick={() => onChange({ ...values, ...resourceFormValues(values.originalResources) })}
              >
                {msg('managedAgents.git.keepResources', 'Keep existing resources')}
              </Button>
            </div>
          ) : null}
          {values.fileResources.length ? (
            <SessionFileResourcesField
              resources={values.fileResources}
              showAddButton={false}
              showHeading={false}
              workspaceId={workspaceId}
              onChange={(fileResources) => patch({ fileResources })}
            />
          ) : null}
          {values.gitResources.map((resource, index) => (
            <GitRepositoryFields
              key={index}
              resource={resource}
              index={index}
              onChange={(updated) =>
                patch({ gitResources: values.gitResources.map((item, i) => (i === index ? updated : item)) })
              }
              onRemove={() => patch({ gitResources: values.gitResources.filter((_, i) => i !== index) })}
            />
          ))}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button type="button" variant="outline" size="sm">
                  <Plus aria-hidden />
                  {msg('managedAgents.sessions.resources.add', 'Add resource')}
                  <ChevronDown aria-hidden />
                </Button>
              }
            />
            <DropdownMenuContent align="start">
              <DropdownMenuItem onClick={() => patch({ gitResources: [...values.gitResources, emptyGitResource()] })}>
                <GitBranch aria-hidden />
                {msg('managedAgents.git.repository', 'Git repository')}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => patch({ fileResources: [...values.fileResources, { fileId: '', mountPath: '' }] })}
              >
                <FileText aria-hidden />
                {msg('managedAgents.sessions.resources.typeFile', 'File')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          {memoryStores ? (
            <DeploymentAddSelectField
              label={msg('managedAgents.memoryStores.title', 'Memory stores')}
              optional
              valueLabel={msg('managedAgents.memoryStores.kind', 'memory store')}
              selectedIds={values.memoryStoreIds}
              options={memoryStores}
              manageHref={`/workspaces/${workspaceId}/memory-stores`}
              manageLabel={msg('managedAgents.memoryStores.manage', 'Manage memory stores')}
              onChange={(memoryStoreIds) => patch({ memoryStoreIds })}
            />
          ) : null}
        </>
      )}
    </section>
  );
}
