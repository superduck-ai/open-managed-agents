import { Check, ChevronsUpDown, ExternalLink, Loader2, Plus } from 'lucide-react';
import { type ReactNode, useState } from 'react';
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
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';

export type CreateDialogPickerOption = {
  id: string;
  label: string;
  description?: string;
  disabled?: boolean;
  icon?: ReactNode;
};

export type CreateDialogPickerListProps = {
  searchPlaceholder: string;
  emptyLabel: string;
  options: CreateDialogPickerOption[];
  selectedIds: string[];
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
  onToggle: (id: string) => void;
  searchValue?: string;
  onSearchChange?: (value: string) => void;
};

export function CreateDialogPicker({
  label,
  placeholder,
  searchPlaceholder,
  emptyLabel,
  options,
  selectedIds,
  loading,
  error,
  onRetry,
  onToggle,
  searchValue,
  onSearchChange,
  createLabel,
  onCreate,
}: {
  label: string;
  placeholder: string;
  searchPlaceholder: string;
  emptyLabel: string;
  options: CreateDialogPickerOption[];
  selectedIds: string[];
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
  onToggle: (id: string) => void;
  searchValue?: string;
  onSearchChange?: (value: string) => void;
  createLabel?: string;
  onCreate?: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            role="combobox"
            aria-expanded={open}
            aria-label={label}
            className="h-9 justify-start gap-2 px-2 text-sm font-normal text-foreground"
          />
        }
      >
        <Plus className="size-4" aria-hidden />
        {placeholder}
        <ChevronsUpDown className="ml-auto size-3.5 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(420px,calc(100vw-2rem))] gap-0 p-0">
        <CreateDialogPickerList
          searchPlaceholder={searchPlaceholder}
          emptyLabel={emptyLabel}
          options={options}
          selectedIds={selectedIds}
          loading={loading}
          error={error}
          onRetry={onRetry}
          onToggle={onToggle}
          searchValue={searchValue}
          onSearchChange={onSearchChange}
        />
        {createLabel && onCreate ? (
          <div className="border-t border-border p-1.5">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-full justify-start"
              onClick={() => {
                onCreate();
                setOpen(false);
              }}
            >
              <ExternalLink className="size-4" aria-hidden />
              {createLabel}
            </Button>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

export function CreateDialogPickerList({
  searchPlaceholder,
  emptyLabel,
  options,
  selectedIds,
  loading,
  error,
  onRetry,
  onToggle,
  searchValue,
  onSearchChange,
}: CreateDialogPickerListProps) {
  const { msg } = useI18n();
  return (
    <Command shouldFilter={!onSearchChange}>
      <CommandInput placeholder={searchPlaceholder} value={searchValue} onValueChange={onSearchChange} />
      <CommandList className="max-h-72">
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" aria-hidden />
            {msg('common.loading', 'Loading...')}
          </div>
        ) : error ? (
          <div className="space-y-3 px-4 py-6 text-center text-sm text-muted-foreground">
            <p>{msg('managedAgents.agents.createDialog.optionsLoadFailed', 'Could not load options.')}</p>
            {onRetry ? (
              <Button type="button" variant="outline" size="sm" onClick={onRetry}>
                {msg('common.retry', 'Retry')}
              </Button>
            ) : null}
          </div>
        ) : (
          <>
            <CommandEmpty>{emptyLabel}</CommandEmpty>
            <CommandGroup>
              {options.map((option) => {
                const selected = selectedIds.includes(option.id);
                return (
                  <CommandItem
                    key={option.id}
                    value={`${option.label} ${option.id}`}
                    disabled={option.disabled}
                    onSelect={() => onToggle(option.id)}
                  >
                    {option.icon ?? (
                      <span
                        className={cn(
                          'flex size-4 items-center justify-center rounded border border-border',
                          selected && 'border-primary bg-primary text-primary-foreground',
                        )}
                      >
                        {selected ? <Check className="size-3" aria-hidden /> : null}
                      </span>
                    )}
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{option.label}</span>
                      {option.description ? (
                        <span className="block truncate text-xs text-muted-foreground">{option.description}</span>
                      ) : null}
                    </span>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </>
        )}
      </CommandList>
    </Command>
  );
}
