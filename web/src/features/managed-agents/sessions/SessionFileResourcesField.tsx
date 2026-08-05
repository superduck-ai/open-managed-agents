import { ChevronDown, FileText, Plus, Trash2 } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { Field, FieldDescription, FieldLabel } from '@/shared/ui/field';
import { Input } from '@/shared/ui/input';
import type { SessionFileResourceFormValue } from '../types';
import { isValidSessionFileMountPath, sessionFileRuntimePath } from './file-resource-path';

export function SessionFileResourcesField({
  resources,
  workspaceId,
  onChange,
}: {
  resources: SessionFileResourceFormValue[];
  workspaceId: string;
  onChange: (resources: SessionFileResourceFormValue[]) => void;
}) {
  const { msg } = useI18n();
  const updateResource = (index: number, patch: Partial<SessionFileResourceFormValue>) => {
    onChange(
      resources.map((resource, resourceIndex) => (resourceIndex === index ? { ...resource, ...patch } : resource)),
    );
  };
  const removeResource = (index: number) => {
    onChange(resources.filter((_, resourceIndex) => resourceIndex !== index));
  };

  return (
    <section className="space-y-3" aria-labelledby="session-resources-title">
      <div>
        <h3 id="session-resources-title" className="text-sm font-semibold text-foreground">
          {msg('managedAgents.sessions.resources.title', 'Resources')}
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          {msg('managedAgents.sessions.resources.description', 'Mount files into the session uploads directory.')}
        </p>
      </div>

      {resources.map((resource, index) => {
        const runtimePath = sessionFileRuntimePath(resource.mountPath);
        return (
          <Card key={index} size="sm" className="gap-3 py-3">
            <CardHeader className="grid-cols-[1fr_auto] items-center px-3">
              <CardTitle className="flex items-center gap-2 text-sm">
                <FileText className="size-4 text-muted-foreground" aria-hidden />
                {msg('managedAgents.sessions.resources.typeFile', 'File')}
              </CardTitle>
              <CardAction>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={msg('managedAgents.sessions.resources.removeFile', 'Remove file resource {index}', {
                    index: index + 1,
                  })}
                  onClick={() => removeResource(index)}
                >
                  <Trash2 aria-hidden />
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="space-y-3 px-3">
              <Field>
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-1">
                    <FieldLabel htmlFor={`session-file-id-${index}`}>
                      {msg('managedAgents.sessions.resources.fileId', 'File ID')}
                    </FieldLabel>
                    <span className="text-destructive" aria-hidden>
                      *
                    </span>
                  </div>
                  <ButtonLink
                    href={`/workspaces/${encodeURIComponent(workspaceId)}/files`}
                    target="_blank"
                    rel="noreferrer"
                    variant="link"
                    size="xs"
                  >
                    {msg('managedAgents.sessions.resources.manageFiles', 'Manage files')}
                  </ButtonLink>
                </div>
                <Input
                  id={`session-file-id-${index}`}
                  value={resource.fileId}
                  placeholder="file_abc123..."
                  required
                  onChange={(event) => updateResource(index, { fileId: event.currentTarget.value })}
                />
              </Field>
              <Field data-invalid={resource.mountPath.length > 0 && !isValidSessionFileMountPath(resource.mountPath)}>
                <div className="flex items-center gap-1">
                  <FieldLabel htmlFor={`session-file-mount-path-${index}`}>
                    {msg('managedAgents.sessions.resources.mountPath', 'Mount path')}
                  </FieldLabel>
                  <span className="text-destructive" aria-hidden>
                    *
                  </span>
                </div>
                <Input
                  id={`session-file-mount-path-${index}`}
                  value={resource.mountPath}
                  placeholder="myfile.txt"
                  required
                  aria-invalid={resource.mountPath.length > 0 && !isValidSessionFileMountPath(resource.mountPath)}
                  onChange={(event) => updateResource(index, { mountPath: event.currentTarget.value })}
                />
                <FieldDescription>
                  {runtimePath
                    ? msg('managedAgents.sessions.resources.runtimePath', 'Available at {path}', {
                        path: runtimePath,
                      })
                    : msg('managedAgents.sessions.resources.mountHelp', 'Enter a path relative to {path}', {
                        path: '/uploads',
                      })}
                </FieldDescription>
              </Field>
            </CardContent>
          </Card>
        );
      })}

      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button type="button" variant="secondary">
              <Plus aria-hidden />
              {msg('managedAgents.sessions.resources.add', 'Add resource')}
              <ChevronDown aria-hidden />
            </Button>
          }
        />
        <DropdownMenuContent align="start">
          <DropdownMenuItem onClick={() => onChange([...resources, { fileId: '', mountPath: '' }])}>
            <FileText aria-hidden />
            {msg('managedAgents.sessions.resources.typeFile', 'File')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </section>
  );
}

export function areSessionFileResourcesValid(resources: SessionFileResourceFormValue[]) {
  return resources.every(
    (resource) => resource.fileId.trim().length > 0 && isValidSessionFileMountPath(resource.mountPath),
  );
}
