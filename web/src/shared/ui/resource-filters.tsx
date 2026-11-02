import { Search, X } from 'lucide-react';
import { type KeyboardEventHandler, useRef } from 'react';

import { useI18n } from '../i18n';
import { cn } from '../lib/utils';
import { Button } from './button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './select';
import { Input } from './input';
import { Label } from './label';

export function ResourceSearchField({
  id,
  value,
  placeholder,
  clearLabel,
  prefix,
  onChange,
  onKeyDown,
}: {
  id: string;
  value: string;
  placeholder: string;
  clearLabel?: string;
  prefix?: string;
  onChange: (value: string) => void;
  onKeyDown?: KeyboardEventHandler<HTMLInputElement>;
}) {
  const { msg } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const clearSearch = () => {
    onChange('');
    inputRef.current?.focus();
  };
  const handleKeyDown: KeyboardEventHandler<HTMLInputElement> = (event) => {
    if (event.key === 'Escape' && value) {
      event.preventDefault();
      clearSearch();
      return;
    }
    onKeyDown?.(event);
  };

  return (
    <div className="relative block h-9 w-[320px] max-w-full">
      <Label className="sr-only" htmlFor={id}>
        {placeholder}
      </Label>
      <Search
        className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground/70"
        aria-hidden
      />
      {prefix ? (
        <span className="pointer-events-none absolute left-10 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
          {prefix}
        </span>
      ) : null}
      <Input
        ref={inputRef}
        type="search"
        data-custom-clear
        id={id}
        value={value}
        placeholder={placeholder}
        className={cn('h-9', prefix ? 'pl-[64px]' : 'pl-9', value ? 'pr-9' : 'pr-3')}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={handleKeyDown}
      />
      {value ? (
        <Button
          type="button"
          aria-label={clearLabel ?? msg('common.clearSearch', 'Clear {placeholder}', { placeholder })}
          variant="ghost"
          size="icon-xs"
          className="absolute right-2 top-1/2 size-5 -translate-y-1/2 text-muted-foreground hover:bg-accent hover:text-foreground"
          onClick={clearSearch}
        >
          <X className="size-4" aria-hidden />
        </Button>
      ) : null}
    </div>
  );
}

export function ResourceFilterDropdown<TValue extends string, TMenu extends string>({
  label,
  valueLabel,
  options,
  value,
  menu,
  open,
  menuWidthClass,
  onOpenChange,
  onSelect,
}: {
  label: string;
  valueLabel: string;
  options: Array<{ value: TValue; label: string }>;
  value: TValue;
  menu: TMenu;
  open: boolean;
  menuWidthClass: string;
  onOpenChange: (menu: TMenu | null) => void;
  onSelect: (value: TValue) => void;
}) {
  return (
    <Select<TValue>
      value={value}
      items={options}
      open={open}
      onOpenChange={(nextOpen) => onOpenChange(nextOpen ? menu : null)}
      onValueChange={(nextValue) => nextValue !== null && onSelect(nextValue)}
    >
      <SelectTrigger aria-label={`${label} ${valueLabel}`} className="h-9 gap-2 px-3" data-resource-filter-menu>
        <span className="text-muted-foreground">{label}</span>
        <SelectValue className="font-medium text-foreground">{valueLabel}</SelectValue>
      </SelectTrigger>
      <SelectContent data-resource-filter-menu className={menuWidthClass}>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value} label={option.label}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
