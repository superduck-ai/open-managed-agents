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
import { Check, ChevronsUpDown, X } from 'lucide-react';
import { useState } from 'react';
import { DeploymentFieldHeader } from '../components/common';
import { type EntityOption } from '../types';
import { vaultCreatedAbsoluteLabel, vaultCreatedLabel, vaultCredentialSummaryFromNames } from './model';

export function ManagedVaultSelectField({
  label,
  optional = false,
  selectedIds,
  options,
  manageHref,
  manageLabel,
  acknowledged = false,
  onAcknowledgedChange,
  onChange,
}: {
  label: string;
  optional?: boolean;
  selectedIds: string[];
  options: EntityOption[];
  manageHref: string;
  manageLabel: string;
  acknowledged?: boolean;
  onAcknowledgedChange?: (value: boolean) => void;
  onChange: (value: string[]) => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [open, setOpen] = useState(false);
  const id = `managed-vault-select-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
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

  const updateSelection = (nextIds: string[]) => {
    onChange(nextIds);
    if (!nextIds.length && acknowledged) {
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
                  variant="outline"
                  role="combobox"
                  aria-expanded={open}
                  aria-label={label}
                  disabled={!options.length}
                  className={cn(
                    'managed-resource-field mt-0 h-10 w-full justify-between gap-2 border-border bg-secondary px-3 text-sm font-normal hover:bg-secondary focus-visible:border-ring focus-visible:ring-0 disabled:cursor-not-allowed',
                    selectedOptions.length ? 'pr-9' : 'pr-3',
                  )}
                />
              }
            >
              <span
                className={cn(
                  'min-w-0 flex-1 truncate text-left',
                  selectedOptions.length ? 'text-foreground' : 'text-muted-foreground',
                )}
              >
                {triggerLabel}
              </span>
              {selectedOptions.length ? null : <ChevronsUpDown className="size-4 shrink-0 opacity-50" aria-hidden />}
            </PopoverTrigger>
            <PopoverContent align="start" className="w-[var(--anchor-width)] min-w-[var(--anchor-width)] gap-0 p-0">
              <Command>
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
                      const names = option.credentialNames ?? [];
                      const trailing = vaultCredentialSummaryFromNames(names, msg);
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
                              'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border border-border',
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
                            <TooltipContent className="max-w-xs whitespace-pre-line">
                              {names.length ? names.join('\n') : trailing}
                            </TooltipContent>
                          </Tooltip>
                        </CommandItem>
                      );
                    })}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>
          {selectedOptions.length ? (
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
