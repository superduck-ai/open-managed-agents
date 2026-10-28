import { Check, ChevronsUpDown, Loader2, X } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { ButtonGroup } from '../../../shared/ui/button-group';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '../../../shared/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';

export interface ScopeOption {
  value: string;
  label: string;
  description?: string;
}

const ALL_VALUE = '__all__';

function OptionItem({ option, selected, onSelect }: { option: ScopeOption; selected: boolean; onSelect: () => void }) {
  return (
    <CommandItem
      value={option.value}
      keywords={[option.label, option.description ?? '']}
      title={option.description ? `${option.label} (${option.description})` : option.label}
      onSelect={onSelect}
    >
      <Check className={cn('size-3.5', selected ? 'opacity-100' : 'opacity-0')} aria-hidden />
      <span className="min-w-0 flex-1 truncate">{option.label}</span>
      {option.description && option.description !== option.label ? (
        <span className="max-w-32 shrink-0 truncate font-mono text-xs text-muted-foreground">{option.description}</span>
      ) : null}
    </CommandItem>
  );
}

export function ScopeCombobox({
  label,
  value,
  allLabel,
  searchPlaceholder,
  emptyLabel,
  options,
  selectedOption,
  loading,
  customPrefix,
  onSearchChange,
  onChange,
}: {
  label: string;
  value: string;
  allLabel: string;
  searchPlaceholder: string;
  emptyLabel: string;
  options: ScopeOption[];
  selectedOption?: ScopeOption;
  loading?: boolean;
  customPrefix?: string;
  onSearchChange?: (search: string) => void;
  onChange: (value: string) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const selected = options.find((option) => option.value === value) ?? selectedOption;
  const triggerLabel = value ? (selected?.label ?? value) : allLabel;
  const custom = search.trim();
  const offerCustom =
    !!customPrefix && custom.startsWith(customPrefix) && !options.some((option) => option.value === custom);
  const changeSearch = (next: string) => {
    setSearch(next);
    onSearchChange?.(next);
  };
  const choose = (next: string) => {
    onChange(next);
    setOpen(false);
    changeSearch('');
  };
  return (
    <ButtonGroup>
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) changeSearch('');
        }}
      >
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="outline"
              role="combobox"
              aria-expanded={open}
              aria-label={label}
              title={selected?.description ? `${triggerLabel} (${selected.description})` : triggerLabel}
              className="w-52 justify-between font-normal"
            />
          }
        >
          <span className={cn('min-w-0 flex-1 truncate text-left', !value && 'text-muted-foreground')}>
            {triggerLabel}
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-80 gap-0 overflow-hidden p-0">
          <Command loop shouldFilter={!onSearchChange} className="rounded-none bg-transparent">
            <CommandInput placeholder={searchPlaceholder} value={search} onValueChange={changeSearch} />
            <CommandList className="subtle-scrollbar max-h-72">
              {loading ? (
                <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" aria-hidden />
                  {msg('common.loading', 'Loading...')}
                </div>
              ) : (
                <>
                  <CommandEmpty>{emptyLabel}</CommandEmpty>
                  {onSearchChange && custom && !options.length ? (
                    <p role="status" className="py-6 text-center text-sm text-muted-foreground">
                      {emptyLabel}
                    </p>
                  ) : null}
                  <CommandGroup>
                    {offerCustom ? (
                      <CommandItem value={custom} onSelect={() => choose(custom)}>
                        <Check className="size-3.5 opacity-0" aria-hidden />
                        <span className="min-w-0 flex-1 truncate">
                          {msg('observability.useCustomValue', 'Use {value}', { value: custom })}
                        </span>
                      </CommandItem>
                    ) : null}
                    <CommandItem value={ALL_VALUE} keywords={[allLabel]} onSelect={() => choose('')}>
                      <Check className={cn('size-3.5', value ? 'opacity-0' : 'opacity-100')} aria-hidden />
                      <span className="truncate">{allLabel}</span>
                    </CommandItem>
                    {options.map((option) => (
                      <OptionItem
                        key={option.value}
                        option={option}
                        selected={value === option.value}
                        onSelect={() => choose(option.value)}
                      />
                    ))}
                  </CommandGroup>
                </>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {value ? (
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={msg('observability.clearFilter', 'Clear {label}', { label })}
          onClick={() => choose('')}
        >
          <X aria-hidden />
        </Button>
      ) : null}
    </ButtonGroup>
  );
}
