import { ChevronDown, FileText, Plus, Trash2 } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useI18n } from '@/shared/i18n';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@/shared/ui/combobox';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { Field, FieldDescription, FieldLabel } from '@/shared/ui/field';
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/shared/ui/input-group';
import { listSessionFileOptions } from '../api';
import type { FileMetadataApiResponse, SessionFileResourceFormValue } from '../types';
import { formatBytes } from '../utils';
import { isValidSessionFileMountPath, SESSION_FILE_UPLOADS_ROOT } from './file-resource-path';

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
  const filesQuery = useQuery({
    queryKey: ['managed-agents', 'session-file-options', workspaceId],
    queryFn: () => listSessionFileOptions(workspaceId),
    enabled: resources.length > 0 && Boolean(workspaceId),
    retry: false,
  });
  const files = filesQuery.data?.data ?? [];
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
        const selectedFilename = files.find((file) => file.id === resource.fileId)?.filename;
        return (
          <Card key={index} size="sm" className="mx-px gap-3 py-3">
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
                <SessionFileCombobox
                  id={`session-file-id-${index}`}
                  files={files}
                  loading={filesQuery.isPending}
                  failed={filesQuery.isError}
                  value={resource.fileId}
                  onChange={(fileId) => updateResource(index, { fileId })}
                />
              </Field>
              <Field data-invalid={resource.mountPath.length > 0 && !isValidSessionFileMountPath(resource.mountPath)}>
                <FieldLabel htmlFor={`session-file-mount-path-${index}`}>
                  {msg('managedAgents.sessions.resources.mountPathOptional', 'Mount path (optional)')}
                </FieldLabel>
                <InputGroup>
                  <InputGroupAddon align="inline-start" className="shrink-0 pr-0">
                    <InputGroupText className="text-foreground">{SESSION_FILE_UPLOADS_ROOT}/</InputGroupText>
                  </InputGroupAddon>
                  <InputGroupInput
                    id={`session-file-mount-path-${index}`}
                    value={resource.mountPath}
                    placeholder={
                      selectedFilename ?? msg('managedAgents.sessions.resources.mountPlaceholder', 'filename')
                    }
                    aria-invalid={resource.mountPath.length > 0 && !isValidSessionFileMountPath(resource.mountPath)}
                    onChange={(event) => updateResource(index, { mountPath: event.currentTarget.value })}
                  />
                </InputGroup>
                <FieldDescription>
                  {msg(
                    'managedAgents.sessions.resources.mountHelp',
                    'Files are mounted in the container under {path}.',
                    {
                      path: `${SESSION_FILE_UPLOADS_ROOT}/`,
                    },
                  )}
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

function SessionFileCombobox({
  failed,
  files,
  id,
  loading,
  onChange,
  value,
}: {
  failed: boolean;
  files: FileMetadataApiResponse[];
  id: string;
  loading: boolean;
  onChange: (fileId: string) => void;
  value: string;
}) {
  const { msg } = useI18n();
  const selectedFile = files.find((file) => file.id === value) ?? null;
  const emptyText = loading
    ? msg('managedAgents.sessions.resources.loadingFiles', 'Loading files...')
    : failed
      ? msg('managedAgents.sessions.resources.loadFilesError', 'Could not load files')
      : msg('managedAgents.sessions.resources.noFiles', 'No files found');

  return (
    <Combobox
      items={files}
      value={selectedFile}
      required
      autoHighlight
      itemToStringLabel={fileOptionLabel}
      itemToStringValue={(file) => file.id}
      isItemEqualToValue={(file, selected) => file.id === selected.id}
      filter={(file, query) => fileSearchText(file).includes(query.toLocaleLowerCase())}
      onValueChange={(file) => onChange(file?.id ?? '')}
    >
      <ComboboxInput
        id={id}
        required
        placeholder={
          loading
            ? msg('managedAgents.sessions.resources.loadingFiles', 'Loading files...')
            : msg('managedAgents.sessions.resources.selectFile', 'Select a file to mount to the session')
        }
        aria-label={msg('managedAgents.sessions.resources.fileId', 'File ID')}
      />
      <ComboboxContent>
        <ComboboxEmpty>{emptyText}</ComboboxEmpty>
        <ComboboxList>
          {(file: FileMetadataApiResponse) => (
            <ComboboxItem key={file.id} value={file}>
              <span className="min-w-0 truncate">{fileOptionLabel(file)}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function fileOptionLabel(file: FileMetadataApiResponse) {
  return `${file.filename} (${formatBytes(file.size_bytes)})`;
}

function fileSearchText(file: FileMetadataApiResponse) {
  return `${file.filename}\n${file.id}`.toLocaleLowerCase();
}

export function areSessionFileResourcesValid(resources: SessionFileResourceFormValue[]) {
  return resources.every(
    (resource) =>
      resource.fileId.trim().length > 0 &&
      (resource.mountPath.trim().length === 0 || isValidSessionFileMountPath(resource.mountPath)),
  );
}
