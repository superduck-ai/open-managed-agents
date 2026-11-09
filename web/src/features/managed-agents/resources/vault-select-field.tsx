import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '../../../shared/ui/command';
import { Label } from '../../../shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../../shared/ui/tooltip';
import { Check, ChevronsUpDown, CircleAlert, Plus, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { DeploymentFieldHeader } from '../components/common';
import { type EntityOption } from '../types';
import { vaultCreatedAbsoluteLabel, vaultCreatedLabel, vaultSelectionEquals } from './model';
import { useVaultCredentialSummaries } from './use-vault-credential-summaries';

function vaultSelectFieldId(label: string) {
  return `managed-vault-select-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
}

export function ManagedVaultSelectField({
  readOnly = false,
  label,
  optional = false,
  selectedIds,
  options,
  ...props
}: {
  label: string;
  optional?: boolean;
  workspaceId: string;
  selectedIds: string[];
  options: EntityOption[];
  manageHref: string;
  manageLabel: string;
  acknowledged?: boolean;
  onAcknowledgedChange?: (value: boolean) => void;
  onChange: (value: string[]) => void;
  presentation?: 'select' | 'add';
  readOnly?: boolean;
}) {
  if (readOnly) {
    return (
      <VaultSelectionReadOnly
        id={vaultSelectFieldId(label)}
        label={label}
        optional={optional}
        options={selectedIds.map(
          (selectedId) => options.find((option) => option.id === selectedId) ?? { id: selectedId, label: selectedId },
        )}
      />
    );
  }
  return (
    <InteractiveVaultSelectField
      label={label}
      optional={optional}
      selectedIds={selectedIds}
      options={options}
      {...props}
    />
  );
}

function InteractiveVaultSelectField({
  label,
  optional = false,
  workspaceId,
  selectedIds,
  options,
  manageHref,
  manageLabel,
  acknowledged = false,
  onAcknowledgedChange,
  onChange,
  presentation = 'select',
}: {
  label: string;
  optional?: boolean;
  workspaceId: string;
  selectedIds: string[];
  options: EntityOption[];
  manageHref: string;
  manageLabel: string;
  acknowledged?: boolean;
  onAcknowledgedChange?: (value: boolean) => void;
  onChange: (value: string[]) => void;
  presentation?: 'select' | 'add';
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [open, setOpen] = useState(false);
  const vaultIds = useMemo(() => options.map((option) => option.id), [options]);
  const { presentationFor } = useVaultCredentialSummaries({
    workspaceId,
    vaultIds,
    enabled: open,
    msg,
  });
  const id = vaultSelectFieldId(label);
  const ackId = `${id}-ack`;
  const selectedOptions = options.filter((option) => selectedIds.includes(option.id));
  const emptyPlaceholder = options.length
    ? msg('managedAgents.credentialVaults.selectOneOrMore', 'Select one or more vaults')
    : msg('managedAgents.common.noValuesAvailable', 'No {label}s available', {
        label: msg('managedAgents.credentialVaults.kind', 'vault'),
      });
  const triggerLabel = selectedOptions.length
    ? selectedOptions.map((option) => option.label).join(', ')
    : emptyPlaceholder;
  const searchPlaceholder = msg(
    'managedAgents.credentialVaults.searchVaultsPlaceholder',
    'Search vaults by name or exact ID',
  );
  const clearLabel = msg('managedAgents.credentialVaults.clearSelection', 'Clear selected vaults');
  const ackTitle = msg('managedAgents.credentialVaults.sessionAck.title', 'I own or am authorized to use this vault.');
  const ackDescription = msg(
    'managedAgents.credentialVaults.sessionAck.description',
    'I understand this means this agent can assume the identity granted by this vault.',
  );
  const loadingSummary = msg('common.loading', 'Loading...');

  const updateSelection = (nextIds: string[]) => {
    const selectionChanged = !vaultSelectionEquals(selectedIds, nextIds);
    onChange(nextIds);
    if (selectionChanged && acknowledged) {
      onAcknowledgedChange?.(false);
    }
  };

  return (
    <div>
      <DeploymentFieldHeader
        id={id}
        label={label}
        optional={optional}
        manageHref={manageHref}
        manageLabel={manageLabel}
      />
      <TooltipProvider>
        <div className="relative">
          <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger
              render={
                <Button
                  type="button"
                  id={id}
                  variant={presentation === 'add' ? 'ghost' : 'outline'}
                  role="combobox"
                  aria-expanded={open}
                  aria-label={label}
                  disabled={presentation === 'select' && !options.length}
                  className={cn(
                    'mt-0 gap-2 text-sm font-normal disabled:cursor-not-allowed',
                    presentation === 'add'
                      ? 'h-9 justify-start px-2'
                      : 'managed-resource-field h-10 w-full justify-between border-border bg-secondary px-3 hover:bg-secondary focus-visible:border-ring focus-visible:ring-0',
                    presentation === 'select' && selectedOptions.length && 'pr-9',
                  )}
                />
              }
            >
              <VaultSelectTriggerContent
                presentation={presentation}
                selected={Boolean(selectedOptions.length)}
                label={triggerLabel}
              />
            </PopoverTrigger>
            <PopoverContent
              align="start"
              className={cn(
                'gap-0 p-0',
                presentation === 'add'
                  ? 'w-[380px] max-w-[calc(100vw-3rem)]'
                  : 'w-[var(--anchor-width)] min-w-[var(--anchor-width)]',
              )}
            >
              <Command
                defaultValue={selectedOptions[0] ? `${selectedOptions[0].label} ${selectedOptions[0].id}` : undefined}
                label={searchPlaceholder}
              >
                <CommandInput placeholder={searchPlaceholder} />
                <CommandList className="max-h-72">
                  <CommandEmpty>
                    {msg('managedAgents.credentialVaults.searchEmpty', 'No vaults match your search.')}
                  </CommandEmpty>
                  <CommandGroup>
                    {options.map((option) => {
                      const selected = selectedIds.includes(option.id);
                      const createdAt = option.createdAt ?? '';
                      const createdRelative = createdAt
                        ? vaultCreatedLabel(createdAt, formatters.relativeTime, formatters.date)
                        : '';
                      const createdAbsolute = createdAt ? vaultCreatedAbsoluteLabel(createdAt, formatters.date) : '';
                      const { trailing, detail } = presentationFor(option.id, loadingSummary);
                      return (
                        <CommandItem
                          key={option.id}
                          value={`${option.label} ${option.id}`}
                          onSelect={() => {
                            updateSelection(
                              selected
                                ? selectedIds.filter((selectedId) => selectedId !== option.id)
                                : [...selectedIds, option.id],
                            );
                          }}
                          className="items-start gap-2 py-2"
                        >
                          <span
                            className={cn(
                              'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border-[0.5px] border-border',
                              selected && 'border-primary bg-primary text-primary-foreground',
                            )}
                            aria-hidden
                          >
                            {selected ? <Check className="size-3" /> : null}
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm text-foreground">{option.label}</span>
                            {createdRelative ? (
                              <Tooltip>
                                <TooltipTrigger
                                  render={
                                    <span className="block truncate text-xs text-muted-foreground">
                                      {createdRelative}
                                    </span>
                                  }
                                />
                                <TooltipContent>{createdAbsolute}</TooltipContent>
                              </Tooltip>
                            ) : null}
                          </span>
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span className="max-w-[40%] shrink-0 truncate text-right text-xs text-muted-foreground">
                                  {trailing}
                                </span>
                              }
                            />
                            <TooltipContent className="max-w-xs whitespace-pre-line">{detail}</TooltipContent>
                          </Tooltip>
                        </CommandItem>
                      );
                    })}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>
          {presentation === 'select' && selectedOptions.length ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={clearLabel}
              className="absolute top-1/2 right-2 size-6 -translate-y-1/2 text-muted-foreground hover:bg-transparent hover:text-foreground"
              onClick={() => updateSelection([])}
            >
              <X className="size-3.5" aria-hidden />
            </Button>
          ) : null}
        </div>
      </TooltipProvider>
      {presentation === 'add' && selectedOptions.length ? (
        <div className="mt-2 flex flex-wrap gap-2">
          {selectedOptions.map((option) => (
            <span
              key={option.id}
              className="inline-flex max-w-full items-center gap-2 rounded-md border-[0.5px] border-border bg-secondary py-1 pl-2 text-sm"
            >
              <span className="truncate">{option.label}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={msg('managedAgents.credentialVaults.removeVault', 'Remove {name}', { name: option.label })}
                onClick={() => updateSelection(selectedIds.filter((id) => id !== option.id))}
              >
                <X aria-hidden />
              </Button>
            </span>
          ))}
        </div>
      ) : null}
      {selectedOptions.length && onAcknowledgedChange ? (
        <div className="mt-3 flex items-start gap-2">
          <Checkbox
            id={ackId}
            checked={acknowledged}
            aria-label={`${ackTitle} ${ackDescription}`}
            className="mt-0.5"
            onCheckedChange={(checked) => onAcknowledgedChange(checked === true)}
          />
          <Label
            htmlFor={ackId}
            className="cursor-pointer flex-col items-start gap-0.5 text-sm font-normal leading-5 text-foreground"
          >
            <span>{ackTitle}</span>
            <span className="text-muted-foreground">{ackDescription}</span>
          </Label>
        </div>
      ) : null}
    </div>
  );
}

function VaultSelectionReadOnly({
  id,
  label,
  optional,
  options,
}: {
  id: string;
  label: string;
  optional: boolean;
  options: EntityOption[];
}) {
  const { msg } = useI18n();
  return (
    <div>
      <DeploymentFieldHeader id={id} label={label} optional={optional} accessory={<VaultLockedHint />} />
      {options.length ? (
        <div className="flex flex-wrap gap-2">
          {options.map((option) => (
            <span
              key={option.id}
              className="inline-flex max-w-full items-center rounded-md border-[0.5px] border-border bg-secondary px-2 py-1 text-sm"
            >
              <span className="truncate">{option.label}</span>
            </span>
          ))}
        </div>
      ) : (
        <p id={id} className="text-sm text-muted-foreground">
          {msg('managedAgents.credentialVaults.emptyTitle', 'No vaults yet')}
        </p>
      )}
    </div>
  );
}

function VaultLockedHint() {
  const { msg } = useI18n();
  const label = msg('managedAgents.credentialVaults.sessionLocked.label', "Why vaults can't be changed");
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className="size-4 text-muted-foreground hover:text-foreground"
            aria-label={label}
          />
        }
      >
        <CircleAlert className="size-3.5" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 text-sm leading-5">
        {msg(
          'managedAgents.credentialVaults.sessionLocked.description',
          "Vaults are attached when the session is created and can't be changed afterward. Create a new session to use a different vault.",
        )}
      </PopoverContent>
    </Popover>
  );
}

function VaultSelectTriggerContent({
  presentation,
  selected,
  label,
}: {
  presentation: 'select' | 'add';
  selected: boolean;
  label: string;
}) {
  const { msg } = useI18n();
  if (presentation === 'add') {
    return (
      <>
        <Plus className="size-4" aria-hidden />
        <span>{msg('managedAgents.credentialVaults.addVault', 'Add vault')}</span>
      </>
    );
  }
  return (
    <>
      <span className={cn('min-w-0 flex-1 truncate text-left', selected ? 'text-foreground' : 'text-muted-foreground')}>
        {label}
      </span>
      {selected ? null : <ChevronsUpDown className="size-4 shrink-0 opacity-50" aria-hidden />}
    </>
  );
}
