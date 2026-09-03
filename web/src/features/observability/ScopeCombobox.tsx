import { Check, ChevronsUpDown, Loader2, X } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { cn } from '../../shared/lib/utils';
import { Button } from '../../shared/ui/button';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '../../shared/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '../../shared/ui/popover';
import { shortenResourceId } from './format';
import type { ObservabilityScopeOption } from './types';

const ALL_VALUE = '__all__';

export function ScopeCombobox({
  label,
  value,
  allLabel,
  searchPlaceholder,
  emptyLabel,
  clearLabel,
  options,
  loading,
  onChange,
}: {
  label: string;
  value: string;
  allLabel: string;
  searchPlaceholder: string;
  emptyLabel: string;
  clearLabel: string;
  options: ObservabilityScopeOption[];
  loading?: boolean;
  onChange: (id: string) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const selected = options.find((option) => option.id === value);
  const triggerLabel = value ? (selected?.label ?? value) : allLabel;
  const triggerTitle = selected?.description ? `${selected.label} (${selected.description})` : triggerLabel;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setSearch('');
        }
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            role="combobox"
            aria-expanded={open}
            aria-label={label}
            title={triggerTitle}
            className="w-56 justify-between font-normal"
          />
        }
      >
        <span className="min-w-0 flex-1 truncate text-left">{triggerLabel}</span>
        {value ? (
          <span
            aria-label={clearLabel}
            className="inline-flex size-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground"
            onPointerDown={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onChange('');
              setOpen(false);
            }}
          >
            <X className="size-3.5" strokeWidth={1.5} aria-hidden />
          </span>
        ) : null}
        <ChevronsUpDown
          className="size-3.5 shrink-0 text-muted-foreground"
          data-icon="inline-end"
          strokeWidth={1.5}
          aria-hidden
        />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 gap-0 overflow-hidden p-0">
        <Command loop className="rounded-none bg-transparent">
          <CommandInput placeholder={searchPlaceholder} value={search} onValueChange={setSearch} />
          <CommandList className="subtle-scrollbar max-h-72">
            {loading ? (
              <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" aria-hidden />
                {msg('common.loading', 'Loading...')}
              </div>
            ) : (
              <>
                <CommandEmpty>{emptyLabel}</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    value={ALL_VALUE}
                    keywords={[allLabel]}
                    onSelect={() => {
                      onChange('');
                      setOpen(false);
                    }}
                  >
                    <Check className={cn('size-3.5', value ? 'opacity-0' : 'opacity-100')} aria-hidden />
                    <span className="truncate">{allLabel}</span>
                  </CommandItem>
                  {options.map((option) => (
                    <ScopeOptionItem
                      key={option.id}
                      option={option}
                      selected={value === option.id}
                      onSelect={() => {
                        onChange(option.id);
                        setOpen(false);
                      }}
                    />
                  ))}
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function ScopeOptionItem({
  option,
  selected,
  onSelect,
}: {
  option: ObservabilityScopeOption;
  selected: boolean;
  onSelect: () => void;
}) {
  const resourceId = option.description || (option.label === option.id ? option.id : undefined);
  return (
    <CommandItem
      value={option.id}
      keywords={[option.label]}
      title={resourceId && resourceId !== option.label ? `${option.label} (${resourceId})` : option.label}
      onSelect={onSelect}
    >
      <Check className={cn('size-3.5', selected ? 'opacity-100' : 'opacity-0')} aria-hidden />
      <span className="min-w-0 flex-1 truncate">{option.label}</span>
      {resourceId && resourceId !== option.label ? (
        <span className="max-w-[45%] shrink-0 truncate font-mono text-xs text-muted-foreground">
          {shortenResourceId(resourceId)}
        </span>
      ) : null}
    </CommandItem>
  );
}
