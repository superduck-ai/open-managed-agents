import { ChevronsUpDown, Plus } from 'lucide-react';
import { type FormEvent, useCallback, useEffect, useId, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Field, FieldDescription, FieldError, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { type CreateAgentInput } from '../types';
import { toRecord } from '../utils';
import { addMcpServer, type McpServerInputErrors } from './create-dialog-model';
import { CreateDialogPickerList } from './create-dialog-picker';
import { type McpDirectoryServer } from './tools/model';
import { RemoteServerIcon } from './tools/RemoteServerIcon';

type McpPickerTab = 'directory' | 'custom';

export function CreateDialogMcpPicker({
  draft,
  directoryServers,
  directoryLoading,
  directoryError,
  onRetryDirectory,
  onChange,
}: {
  draft: CreateAgentInput;
  directoryServers: McpDirectoryServer[];
  directoryLoading: boolean;
  directoryError: boolean;
  onRetryDirectory: () => void;
  onChange: (next: CreateAgentInput) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [activeTab, setActiveTab] = useState<McpPickerTab>('directory');
  const [searchValue, setSearchValue] = useState('');
  const [name, setName] = useState('');
  const [url, setURL] = useState('');
  const [errors, setErrors] = useState<McpServerInputErrors>({});
  const [directoryAddFailed, setDirectoryAddFailed] = useState(false);
  const nameInputRef = useRef<HTMLInputElement>(null);
  const fieldID = useId();
  const atLimit = draft.mcp_servers.length >= 20;
  const configuredServerNames = draft.mcp_servers.flatMap((server) => {
    const serverName = toRecord(server)?.name;
    return typeof serverName === 'string' ? [serverName] : [];
  });
  const availableServers = directoryServers.filter(
    (server) => server.url && !configuredServerNames.includes(server.slug),
  );
  const normalizedSearch = searchValue.trim().toLowerCase();
  const filteredServers = normalizedSearch
    ? availableServers.filter((server) =>
        `${server.displayName} ${server.slug} ${server.url}`.toLowerCase().includes(normalizedSearch),
      )
    : availableServers;

  useEffect(() => {
    if (open && activeTab === 'custom') {
      nameInputRef.current?.focus();
    }
  }, [activeTab, open]);

  const reset = useCallback(() => {
    setActiveTab('directory');
    setSearchValue('');
    setName('');
    setURL('');
    setErrors({});
    setDirectoryAddFailed(false);
  }, []);

  const close = useCallback(() => {
    setOpen(false);
    reset();
  }, [reset]);

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    if (!nextOpen) {
      reset();
    }
  };

  const addDirectoryServer = (id: string) => {
    const server = availableServers.find((candidate) => candidate.slug === id);
    if (!server?.url) {
      return;
    }
    const result = addMcpServer(draft, { name: server.slug, url: server.url });
    if (!result.ok) {
      setDirectoryAddFailed(true);
      return;
    }
    onChange(result.draft);
    close();
  };

  const addCustomServer = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const result = addMcpServer(draft, { name, url });
    if (!result.ok) {
      setErrors(result.errors);
      return;
    }
    onChange(result.draft);
    close();
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            role="combobox"
            aria-expanded={open}
            aria-label={msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}
            className="h-9 justify-start gap-2 px-2 text-sm font-normal text-foreground"
          />
        }
      >
        <Plus className="size-4" aria-hidden />
        {msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}
        <ChevronsUpDown className="ml-auto size-3.5 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(420px,calc(100vw-2rem))] gap-0 p-0">
        <Tabs value={activeTab} className="gap-0" onValueChange={(value) => setActiveTab(value as McpPickerTab)}>
          <div className="border-b border-border p-2">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="directory">
                {msg('managedAgents.agents.createDialog.mcpDirectoryTab', 'Directory')}
              </TabsTrigger>
              <TabsTrigger value="custom">
                {msg('managedAgents.agents.createDialog.customMcpTab', 'Custom MCP')}
              </TabsTrigger>
            </TabsList>
          </div>
          <TabsContent value="directory" className="mt-0">
            <CreateDialogPickerList
              searchPlaceholder={msg('managedAgents.agents.createDialog.searchMcpServers', 'Search MCP servers...')}
              emptyLabel={msg('managedAgents.agents.createDialog.noMcpServers', 'No MCP servers found.')}
              options={filteredServers.map((server) => ({
                id: server.slug,
                label: server.displayName,
                description: server.url,
                disabled: atLimit,
                icon: <RemoteServerIcon directoryIconUrl={server.iconUrl} className="size-8" iconClassName="size-4" />,
              }))}
              selectedIds={[]}
              loading={directoryLoading}
              error={directoryError}
              onRetry={onRetryDirectory}
              onToggle={addDirectoryServer}
              searchValue={searchValue}
              onSearchChange={setSearchValue}
            />
            {atLimit ? (
              <p role="alert" className="border-t border-border px-3 py-2 text-sm text-destructive">
                {msg('managedAgents.agents.createDialog.mcpServerLimit', 'An agent can use at most 20 MCP servers.')}
              </p>
            ) : null}
            {directoryAddFailed ? (
              <p role="alert" className="border-t border-border px-3 py-2 text-sm text-destructive">
                {msg('managedAgents.agents.createDialog.mcpAddFailed', 'Could not add this MCP server.')}
              </p>
            ) : null}
          </TabsContent>
          <TabsContent value="custom" className="mt-0">
            <form className="space-y-4 p-4" onSubmit={addCustomServer}>
              <Field data-invalid={Boolean(errors.name)}>
                <FieldLabel htmlFor={`${fieldID}-name`}>
                  {msg('managedAgents.agents.createDialog.customMcpName', 'Name')}
                </FieldLabel>
                <Input
                  ref={nameInputRef}
                  id={`${fieldID}-name`}
                  value={name}
                  placeholder="internal-docs"
                  aria-invalid={Boolean(errors.name) || undefined}
                  onChange={(event) => {
                    setName(event.target.value);
                    setErrors((current) => ({ ...current, name: undefined }));
                  }}
                />
                <FieldError>{mcpInputError('name', errors.name, msg)}</FieldError>
              </Field>
              <Field data-invalid={Boolean(errors.url)}>
                <FieldLabel htmlFor={`${fieldID}-url`}>
                  {msg('managedAgents.agents.createDialog.customMcpUrl', 'MCP Server URL')}
                </FieldLabel>
                <Input
                  id={`${fieldID}-url`}
                  value={url}
                  inputMode="url"
                  placeholder="https://mcp.example.com/mcp"
                  aria-invalid={Boolean(errors.url) || undefined}
                  onChange={(event) => {
                    setURL(event.target.value);
                    setErrors((current) => ({ ...current, url: undefined }));
                  }}
                />
                <FieldDescription>
                  {msg('managedAgents.agents.createDialog.customMcpUrlHint', 'Only HTTP and HTTPS URLs are supported.')}
                </FieldDescription>
                <FieldError>{mcpInputError('url', errors.url, msg)}</FieldError>
              </Field>
              {errors.form ? (
                <FieldError>
                  {msg('managedAgents.agents.createDialog.mcpServerLimit', 'An agent can use at most 20 MCP servers.')}
                </FieldError>
              ) : null}
              <div className="flex justify-end gap-2">
                <Button type="button" variant="outline" onClick={close}>
                  {msg('common.cancel', 'Cancel')}
                </Button>
                <Button type="submit">{msg('managedAgents.agents.createDialog.addMcpServer', 'Add MCP server')}</Button>
              </div>
            </form>
          </TabsContent>
        </Tabs>
      </PopoverContent>
    </Popover>
  );
}

function mcpInputError(
  field: 'name' | 'url',
  error: McpServerInputErrors[typeof field],
  msg: ReturnType<typeof useI18n>['msg'],
) {
  if (!error) {
    return null;
  }
  if (field === 'name') {
    switch (error) {
      case 'required':
        return msg('managedAgents.agents.createDialog.customMcpNameRequired', 'Name is required.');
      case 'too_long':
        return msg('managedAgents.agents.createDialog.customMcpNameTooLong', 'Name must be at most 255 characters.');
      case 'invalid':
        return msg(
          'managedAgents.agents.createDialog.customMcpNameInvalid',
          'Use only letters, numbers, underscores, hyphens, and periods.',
        );
      case 'ambiguous':
        return msg(
          'managedAgents.agents.createDialog.customMcpNameAmbiguous',
          'Name must not contain two consecutive underscores.',
        );
      default:
        return msg('managedAgents.agents.createDialog.customMcpNameDuplicate', 'This MCP server name is already used.');
    }
  }
  switch (error) {
    case 'required':
      return msg('managedAgents.agents.createDialog.customMcpUrlRequired', 'MCP Server URL is required.');
    case 'too_long':
      return msg(
        'managedAgents.agents.createDialog.customMcpUrlTooLong',
        'MCP Server URL must be at most 2048 characters.',
      );
    default:
      return msg(
        'managedAgents.agents.createDialog.customMcpUrlInvalid',
        'Enter a valid HTTP or HTTPS MCP Server URL.',
      );
  }
}
