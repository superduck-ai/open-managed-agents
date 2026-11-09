import * as React from 'react';
import { CheckIcon, ChevronsUpDownIcon } from 'lucide-react';

import { useI18n } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Button } from './button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from './command';
import { Popover, PopoverContent, PopoverTrigger } from './popover';

const SelectContext = React.createContext<{
  value: string | null;
  disabled: boolean;
  open: boolean;
  emptyLabel?: React.ReactNode;
  labels: ReadonlyMap<string, React.ReactNode>;
  select: (value: string) => void;
} | null>(null);

function useSelect() {
  const context = React.useContext(SelectContext);
  if (!context) throw new Error('Select components require a Select parent.');
  return context;
}

function Select<Value extends string = string>({
  children,
  value,
  defaultValue = null,
  onValueChange,
  items,
  disabled = false,
  open,
  onOpenChange,
  emptyLabel,
}: {
  children?: React.ReactNode;
  value?: Value | null;
  defaultValue?: Value | null;
  onValueChange?: (value: Value | null) => void;
  items?: ReadonlyArray<{ value: Value; label: React.ReactNode }>;
  disabled?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  emptyLabel?: React.ReactNode;
}) {
  const [internalValue, setInternalValue] = React.useState<Value | null>(defaultValue);
  const [internalOpen, setInternalOpen] = React.useState(false);
  const isOpen = open ?? internalOpen;
  const changeOpen = (next: boolean) => {
    setInternalOpen(next);
    onOpenChange?.(next);
  };
  const selectedValue = value === undefined ? internalValue : value;
  const labels = new Map<string, React.ReactNode>(items?.map((item) => [item.value, item.label]));
  const select = (next: string) => {
    if (value === undefined) setInternalValue(next as Value);
    onValueChange?.(next as Value);
    changeOpen(false);
  };
  return (
    <SelectContext.Provider value={{ value: selectedValue, disabled, open: isOpen, labels, select, emptyLabel }}>
      <Popover open={isOpen} onOpenChange={changeOpen}>
        {children}
      </Popover>
    </SelectContext.Provider>
  );
}

function SelectGroup(props: React.ComponentProps<typeof CommandGroup>) {
  return <CommandGroup data-slot="select-group" {...props} />;
}

function SelectValue({
  className,
  children,
  placeholder,
  ...props
}: Omit<React.ComponentProps<'span'>, 'children'> & {
  children?: React.ReactNode | ((value: string | null) => React.ReactNode);
  placeholder?: React.ReactNode;
}) {
  const { value, labels } = useSelect();
  const content = typeof children === 'function' ? children(value) : children;
  return (
    <span data-slot="select-value" className={cn('min-w-0 flex-1 truncate text-left', className)} {...props}>
      {content ?? (value === null ? placeholder : (labels.get(value) ?? value))}
    </span>
  );
}

function SelectTrigger({
  className,
  size = 'default',
  children,
  disabled,
  ...props
}: Omit<React.ComponentProps<typeof Button>, 'size'> & {
  size?: 'sm' | 'default';
}) {
  const context = useSelect();
  return (
    <PopoverTrigger
      data-slot="select-trigger"
      render={
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={context.open}
          disabled={context.disabled || disabled}
          className={cn(
            'w-fit justify-between gap-1.5 font-normal',
            size === 'sm' ? 'h-7 rounded-[7px]' : 'h-8',
            className,
          )}
          {...props}
        />
      }
    >
      {children}
      <ChevronsUpDownIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
    </PopoverTrigger>
  );
}

function SelectContent({
  className,
  children,
  align = 'start',
  alignItemWithTrigger = false,
  ...props
}: React.ComponentProps<typeof PopoverContent> & {
  alignItemWithTrigger?: boolean;
}) {
  const { msg } = useI18n();
  const { value, emptyLabel } = useSelect();
  const searchLabel = msg('select.search', 'Search options...');
  return (
    <PopoverContent
      data-slot="select-content"
      data-align-trigger={alignItemWithTrigger}
      align={align}
      className={cn('w-(--anchor-width) min-w-48 gap-0 overflow-hidden p-0', className)}
      {...props}
    >
      <Command label={searchLabel} defaultValue={value === null ? undefined : JSON.stringify(value)}>
        <CommandInput placeholder={searchLabel} />
        <CommandList className="max-h-72 p-1 [&_[cmdk-group]]:p-0">
          <CommandEmpty>{emptyLabel ?? msg('select.empty', 'No matching options.')}</CommandEmpty>
          {children}
        </CommandList>
      </Command>
    </PopoverContent>
  );
}

function SelectLabel({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div data-slot="select-label" className={cn('px-2 py-1 text-xs text-muted-foreground', className)} {...props} />
  );
}

function optionText(children: React.ReactNode): string {
  return React.Children.toArray(children)
    .map((child) =>
      React.isValidElement<{ children?: React.ReactNode }>(child)
        ? optionText(child.props.children)
        : typeof child === 'string' || typeof child === 'number'
          ? String(child)
          : '',
    )
    .join(' ');
}

function SelectItem({
  className,
  children,
  value,
  label,
  ...props
}: Omit<React.ComponentProps<typeof CommandItem>, 'value' | 'onSelect'> & {
  value: string;
  label?: string;
}) {
  const context = useSelect();
  return (
    <CommandItem
      data-slot="select-item"
      value={JSON.stringify(value)}
      keywords={[value, label ?? optionText(children)]}
      className={cn('w-full min-w-0', className)}
      onSelect={() => context.select(value)}
      {...props}
    >
      <span className="flex min-w-0 flex-1 items-center gap-2 truncate">{children}</span>
      {context.value === value ? <CheckIcon className="size-4 shrink-0" aria-hidden /> : null}
    </CommandItem>
  );
}

function SelectSeparator(props: React.ComponentProps<typeof CommandSeparator>) {
  return <CommandSeparator data-slot="select-separator" {...props} />;
}

export { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectSeparator, SelectTrigger, SelectValue };
