import { Check, ChevronDown, Globe, X } from 'lucide-react';
import { useEffect, useId, useState } from 'react';

import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '../../../shared/ui/command';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '../../../shared/ui/input-group';
import { Label } from '../../../shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { type EntityOption } from '../types';
import { ManagedTextField } from '../components/common';

export const CUSTOM_MCP_SERVER_OPTION_ID = '__custom_mcp_server__';

type McpServerPickerPhase = 'pick' | 'custom' | 'locked';

function normalizeMcpServerDraft(value: string): string {
  return value.trim();
}

function looksLikeMcpServerUrl(value: string): boolean {
  const trimmed = normalizeMcpServerDraft(value);
  if (!trimmed) {
    return false;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
}

export function CredentialMcpServerField({
  value,
  directoryOptions,
  readOnly = false,
  onChange,
}: {
  value: string;
  directoryOptions: EntityOption[];
  readOnly?: boolean;
  onChange: (value: string) => void;
}) {
  const { msg } = useI18n();
  const fieldId = `credential-mcp-server-${useId()}`;
  const [phase, setPhase] = useState<McpServerPickerPhase>(() => (value.trim() ? 'locked' : 'pick'));
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [customDraft, setCustomDraft] = useState(value);

  useEffect(() => {
    if (readOnly) {
      return;
    }
    if (value.trim()) {
      setPhase('locked');
      setCustomDraft(value);
      return;
    }
    setPhase((current) => (current === 'custom' ? current : 'pick'));
  }, [readOnly, value]);

  const label = msg('managedAgents.credentialVaults.credentialDialog.mcpServer', 'MCP server');
  const customLabel = msg('managedAgents.credentialVaults.credentialDialog.mcpCustomServer', 'Custom server');
  const selectedDirectory = directoryOptions.find((option) => option.id === value);

  if (readOnly) {
    return (
      <ManagedTextField
        label={label}
        value={value}
        placeholder={msg(
          'managedAgents.credentialVaults.credentialDialog.mcpServerUrlPlaceholder',
          'https://example.com/mcp',
        )}
        disabled
        onChange={() => undefined}
      />
    );
  }

  if (phase === 'locked' && value.trim()) {
    return (
      <div>
        <Label htmlFor={fieldId} className="text-sm font-medium text-foreground">
          {label}
        </Label>
        <InputGroup className="managed-resource-field mt-2 h-10 border-border bg-secondary shadow-none dark:bg-secondary">
          <InputGroupAddon align="inline-start">
            <Globe className="size-4 text-muted-foreground" aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            id={fieldId}
            readOnly
            value={selectedDirectory?.label ? `${selectedDirectory.label} · ${value}` : value}
            aria-label={label}
            className="truncate bg-transparent"
          />
          <InputGroupAddon align="inline-end">
            <InputGroupButton
              type="button"
              size="icon-xs"
              aria-label={msg('managedAgents.credentialVaults.credentialDialog.clearMcpServer', 'Clear MCP server')}
              onClick={() => {
                onChange('');
                setCustomDraft('');
                setSearch('');
                setPhase('pick');
              }}
            >
              <X className="size-3.5" aria-hidden />
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
      </div>
    );
  }

  if (phase === 'custom') {
    const confirmCustom = () => {
      const next = normalizeMcpServerDraft(customDraft);
      if (!looksLikeMcpServerUrl(next)) {
        return;
      }
      onChange(next);
      setPhase('locked');
      setOpen(false);
      setSearch('');
    };
    return (
      <div>
        <Label htmlFor={fieldId} className="text-sm font-medium text-foreground">
          {label}
        </Label>
        <InputGroup className="managed-resource-field mt-2 h-10 border-border bg-secondary shadow-none dark:bg-secondary">
          <InputGroupAddon align="inline-start">
            <Globe className="size-4 text-muted-foreground" aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            id={fieldId}
            autoFocus
            value={customDraft}
            placeholder={msg(
              'managedAgents.credentialVaults.credentialDialog.mcpServerUrlPlaceholder',
              'https://example.com/mcp',
            )}
            aria-label={label}
            className="bg-transparent"
            onChange={(event) => setCustomDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                confirmCustom();
              }
              if (event.key === 'Escape') {
                event.preventDefault();
                setCustomDraft('');
                setPhase('pick');
              }
            }}
          />
          <InputGroupAddon align="inline-end">
            <InputGroupButton
              type="button"
              size="icon-xs"
              aria-label={msg('managedAgents.credentialVaults.credentialDialog.clearMcpServer', 'Clear MCP server')}
              onClick={() => {
                setCustomDraft('');
                setPhase('pick');
              }}
            >
              <X className="size-3.5" aria-hidden />
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
        <p className="mt-1.5 text-xs text-muted-foreground">
          {msg(
            'managedAgents.credentialVaults.credentialDialog.mcpCustomServerHint',
            'Enter an HTTPS MCP URL and press Enter to confirm.',
          )}
        </p>
      </div>
    );
  }

  return (
    <div>
      <Label htmlFor={fieldId} className="text-sm font-medium text-foreground">
        {label}
      </Label>
      <Popover
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen);
          if (!nextOpen) {
            setSearch('');
          }
        }}
      >
        <PopoverTrigger
          render={
            <Button
              id={fieldId}
              type="button"
              variant="outline"
              role="combobox"
              aria-expanded={open}
              aria-label={label}
              className="managed-resource-field mt-2 h-10 w-full justify-between border-border bg-secondary px-3 text-sm font-normal text-foreground shadow-none hover:bg-secondary focus-visible:border-ring focus-visible:ring-0"
            />
          }
        >
          <span className="truncate text-muted-foreground">
            {msg(
              'managedAgents.credentialVaults.credentialDialog.mcpDirectoryPlaceholder',
              'Choose from directory or custom server',
            )}
          </span>
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          sideOffset={4}
          className="w-[min(var(--anchor-width),calc(100vw-2rem))] min-w-[var(--anchor-width)] gap-0 overflow-hidden rounded-lg p-0 shadow-md ring-1 ring-foreground/10"
        >
          <Command className="rounded-none bg-transparent" shouldFilter>
            <CommandInput
              value={search}
              onValueChange={setSearch}
              placeholder={msg(
                'managedAgents.credentialVaults.credentialDialog.mcpDirectorySearch',
                'Search MCP directory...',
              )}
              className="h-9"
            />
            <CommandList className="max-h-60">
              <CommandEmpty>
                {msg('managedAgents.credentialVaults.credentialDialog.mcpDirectoryEmpty', 'No MCP servers found.')}
              </CommandEmpty>
              <CommandGroup>
                {directoryOptions.map((option) => (
                  <CommandItem
                    key={option.id}
                    value={`${option.label} ${option.id} ${option.secondary ?? ''}`}
                    onSelect={() => {
                      onChange(option.id);
                      setPhase('locked');
                      setOpen(false);
                      setSearch('');
                    }}
                  >
                    <Check className="size-4 opacity-0" aria-hidden />
                    <span className="flex min-w-0 flex-col">
                      <span className="truncate">{option.label}</span>
                      {option.secondary || option.id !== option.label ? (
                        <span className="truncate text-xs text-muted-foreground">{option.secondary ?? option.id}</span>
                      ) : null}
                    </span>
                  </CommandItem>
                ))}
                <CommandItem
                  value={`${customLabel} ${CUSTOM_MCP_SERVER_OPTION_ID} ${search}`}
                  onSelect={() => {
                    const draft = looksLikeMcpServerUrl(search) ? normalizeMcpServerDraft(search) : search.trim();
                    setCustomDraft(draft);
                    setPhase('custom');
                    setOpen(false);
                    setSearch('');
                  }}
                >
                  <Globe className={cn('size-4 text-muted-foreground')} aria-hidden />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{customLabel}</span>
                    {search.trim() ? (
                      <span className="truncate text-xs text-muted-foreground">{search.trim()}</span>
                    ) : (
                      <span className="truncate text-xs text-muted-foreground">
                        {msg(
                          'managedAgents.credentialVaults.credentialDialog.mcpCustomServerDescription',
                          'Enter a custom MCP server URL',
                        )}
                      </span>
                    )}
                  </span>
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}
