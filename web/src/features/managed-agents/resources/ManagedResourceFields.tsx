import { ChevronDown, Database, FileText, GitBranch, Plus } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import type { EntityOption, ManagedEntityFormValues } from '../types';
import { SessionFileResourcesField } from '../sessions/SessionFileResourcesField';
import { emptyMemoryAttach, MAX_MEMORY_ATTACHES } from './memory-attach';
import { GitRepositoryFields } from './GitRepositoryFields';
import { emptyGitResource, resourceFormValues } from './git-resource';

export function ManagedResourceFields({
  values,
  onChange,
  workspaceId,
  editing = false,
  embedded = false,
  memoryStores,
}: {
  values: ManagedEntityFormValues;
  onChange: (values: ManagedEntityFormValues) => void;
  workspaceId: string;
  editing?: boolean;
  embedded?: boolean;
  memoryStores?: EntityOption[];
}) {
  const { msg } = useI18n();
  const patch = (value: Partial<ManagedEntityFormValues>) => onChange({ ...values, ...value, resourcesChanged: true });
  return (
    <section className="space-y-3">
      {embedded ? null : (
        <div>
          <h3 className="text-sm font-semibold">{msg('managedAgents.sessions.resources.title', 'Resources')}</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {msg(
              'managedAgents.git.resourcesHelp',
              'Mount files, Git repositories, or memory stores into the session.',
            )}
          </p>
        </div>
      )}
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
          {values.fileResources.length || values.memoryAttaches.length ? (
            <SessionFileResourcesField
              resources={values.fileResources}
              memoryAttaches={values.memoryAttaches}
              memoryStoreOptions={memoryStores}
              onMemoryAttachesChange={(memoryAttaches) => patch({ memoryAttaches })}
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
              {memoryStores ? (
                <DropdownMenuItem
                  disabled={values.memoryAttaches.length >= MAX_MEMORY_ATTACHES}
                  onClick={() => patch({ memoryAttaches: [...values.memoryAttaches, emptyMemoryAttach()] })}
                >
                  <Database aria-hidden />
                  {msg('managedAgents.memoryStores.kindTitle', 'Memory store')}
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      )}
    </section>
  );
}
