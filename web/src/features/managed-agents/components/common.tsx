import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '../../../shared/ui/alert-dialog';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { Textarea } from '../../../shared/ui/textarea';
import clsx from 'clsx';
import {
  AlertCircle,
  Archive,
  ArrowUpRight,
  Bot,
  ChevronDown,
  Plus,
  Search,
  Trash2,
  TriangleAlert,
  X,
} from 'lucide-react';
import { type FormEvent, type KeyboardEventHandler, type ReactNode, useId, useRef, useState } from 'react';
import { compactAgentId } from '../agents/AgentsResourcePage';
import { entityKindLabel, resourceEmptyAction, resourceEmptyBody, resourceEmptyTitle } from '../labels';
import { entityDisplayName } from '../resources/ManagedResources';
import {
  type AgentApiResponse,
  type EntityOption,
  type IconComponent,
  type ManagedEntityApiResponse,
  type ManagedEntitySection,
  type MemoryApiResponse,
  type ResourceConfig,
  type VaultCredentialApiResponse,
} from '../types';

export function ManagedSearchField({
  id,
  value,
  placeholder,
  prefix,
  onChange,
  onKeyDown,
}: {
  id: string;
  value: string;
  placeholder: string;
  prefix?: string;
  onChange: (value: string) => void;
  onKeyDown?: KeyboardEventHandler<HTMLInputElement>;
}) {
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
        <span className="pointer-events-none absolute left-10 top-1/2 -translate-y-1/2 text-sm text-muted-foreground/70">
          {prefix}
        </span>
      ) : null}
      <Input
        ref={inputRef}
        id={id}
        value={value}
        placeholder={placeholder}
        className={clsx(
          'h-9 border-border bg-secondary text-sm text-foreground placeholder:text-muted-foreground focus-visible:border-border focus-visible:ring-0',
          prefix ? 'pl-[64px]' : 'pl-9',
          value ? 'pr-9' : 'pr-3',
        )}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={handleKeyDown}
      />
      {value ? (
        <Button
          type="button"
          aria-label={`Clear ${placeholder}`}
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

export function AgentFilterDropdown<TValue extends string, TMenu extends string>({
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
    <DropdownMenu open={open} onOpenChange={(nextOpen) => onOpenChange(nextOpen ? menu : null)}>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className={clsx('h-9 gap-2 bg-secondary px-3 text-sm', open && 'border-border')}
            data-agent-filter-menu
          />
        }
      >
        <span className="text-muted-foreground">{label}</span>
        <span className="font-medium text-foreground">{valueLabel}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent data-agent-filter-menu align="start" sideOffset={8} className={menuWidthClass}>
        <DropdownMenuRadioGroup value={value} onValueChange={(nextValue) => onSelect(nextValue as TValue)}>
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value} className="h-11 pl-3 pr-8 text-[15px]">
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function DetailKV({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="mt-3">
      <div className="text-xs font-medium uppercase text-muted-foreground/70">{label}</div>
      <div className="mt-1 text-sm text-foreground">{value}</div>
    </div>
  );
}

export function DetailCard({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="mb-6">
      <div className="mb-3 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-[20px] font-semibold leading-6 text-foreground">{title}</h2>
          {description ? (
            <p className="mt-1 max-w-[760px] text-sm leading-5 text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

export function DetailTableCard({
  title,
  description,
  loading,
  error,
  emptyTitle,
  columns,
  rows,
}: {
  title: string;
  description?: string;
  loading: boolean;
  error: string | null;
  emptyTitle: string;
  columns: string[];
  rows: ReactNode[][];
}) {
  return (
    <DetailCard title={title} description={description}>
      <NestedRows loading={loading} error={error} emptyTitle={emptyTitle} columns={columns} rows={rows} />
    </DetailCard>
  );
}

export function ManagedErrorAlert({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Alert variant="destructive" className={className}>
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

export function ManagedWarningAlert({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Alert className={clsx('border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400', className)}>
      <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
      <AlertDescription className="text-inherit">{children}</AlertDescription>
    </Alert>
  );
}

export function NestedRows({
  loading,
  error,
  emptyTitle,
  columns,
  rows,
}: {
  loading: boolean;
  error: string | null;
  emptyTitle: string;
  columns: string[];
  rows: ReactNode[][];
}) {
  if (error) {
    return <ManagedErrorAlert>{error}</ManagedErrorAlert>;
  }
  if (loading) {
    return (
      <div className="rounded-lg border border-border bg-card px-4 py-12 text-center text-sm text-muted-foreground">
        Loading...
      </div>
    );
  }
  if (!rows.length) {
    return (
      <div className="rounded-lg border border-border bg-card px-4 py-12 text-center text-sm text-muted-foreground">
        {emptyTitle}
      </div>
    );
  }
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <Table className="table-fixed text-left">
        <TableHeader className="bg-card-raised text-muted-foreground">
          <TableRow className="hover:bg-transparent">
            {columns.map((column) => (
              <TableHead key={column} className="px-3 text-muted-foreground">
                {column}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, rowIndex) => (
            <TableRow key={rowIndex} className="bg-card text-foreground hover:bg-card">
              {row.map((cell, cellIndex) => (
                <TableCell key={cellIndex} className="h-11 truncate px-3">
                  {cell}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

export function MenuAction({
  icon: Icon,
  label,
  disabled = false,
  danger = false,
  onClick,
}: {
  icon: IconComponent;
  label: string;
  disabled?: boolean;
  danger?: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      role="menuitem"
      disabled={disabled}
      variant="ghost"
      className={clsx(
        'h-9 w-full justify-start px-2 text-left hover:bg-accent disabled:cursor-not-allowed disabled:text-muted-foreground/70',
        danger ? 'text-destructive' : 'text-foreground',
      )}
      onClick={onClick}
    >
      <Icon className="size-4" aria-hidden />
      {label}
    </Button>
  );
}

export function LockedAgentReferenceField({
  agent,
  variant,
}: {
  agent: AgentApiResponse;
  variant: 'deployment' | 'managed';
}) {
  const { msg } = useI18n();
  const label = msg('managedAgents.common.agent', 'Agent');
  const body = (
    <div className="flex min-h-10 items-center justify-between gap-3 rounded-lg border border-border bg-secondary px-3 py-2 text-sm text-foreground">
      <span className="min-w-0 truncate font-medium">{agent.name || agent.id}</span>
      <span className="shrink-0 text-xs text-muted-foreground">
        v{agent.version} · {compactAgentId(agent.id)}
      </span>
    </div>
  );

  if (variant === 'deployment') {
    return (
      <div>
        <DeploymentFieldHeader id="locked-agent-reference" label={label} />
        {body}
      </div>
    );
  }

  return (
    <div>
      <div className="mb-1 text-xs font-semibold uppercase text-muted-foreground/70">{label}</div>
      {body}
    </div>
  );
}

export function DeploymentFieldHeader({
  id,
  label,
  optional = false,
  manageHref,
  manageLabel,
}: {
  id: string;
  label: string;
  optional?: boolean;
  manageHref?: string;
  manageLabel?: string;
}) {
  const { msg } = useI18n();

  return (
    <div className="mb-2 flex items-center justify-between gap-4">
      <Label htmlFor={id} className="text-sm font-medium leading-5 text-foreground">
        {label}{' '}
        {optional ? (
          <span className="font-normal text-muted-foreground">
            {msg('managedAgents.common.optionalParen', '(optional)')}
          </span>
        ) : null}
      </Label>
      {manageHref && manageLabel ? (
        <a
          href={manageHref}
          target="_blank"
          rel="noreferrer"
          aria-label={msg('managedAgents.common.opensInNewTab', '{label} (opens in new tab)', { label: manageLabel })}
          className="inline-flex items-center gap-0.5 text-xs leading-4 text-[#6da7ec] underline-offset-2 hover:underline"
        >
          {manageLabel}
          <ArrowUpRight className="size-3" aria-hidden />
        </a>
      ) : null}
    </div>
  );
}

export function DeploymentTextField({
  label,
  value,
  placeholder,
  autoFocus = false,
  onChange,
}: {
  label: string;
  value: string;
  placeholder?: string;
  autoFocus?: boolean;
  onChange: (value: string) => void;
}) {
  const id = `deployment-field-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
  return (
    <div>
      <DeploymentFieldHeader id={id} label={label} />
      <Input
        id={id}
        value={value}
        placeholder={placeholder}
        autoFocus={autoFocus}
        className="h-8 border-white/10 bg-transparent px-3 text-sm text-foreground ring-1 ring-white/10 placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

export function DeploymentTextArea({
  label,
  value,
  placeholder,
  helpText,
  onChange,
}: {
  label: string;
  value: string;
  placeholder?: string;
  helpText?: string;
  onChange: (value: string) => void;
}) {
  const id = `deployment-field-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
  return (
    <div>
      <DeploymentFieldHeader id={id} label={label} />
      <Textarea
        id={id}
        value={value}
        rows={2}
        placeholder={placeholder}
        className="min-h-14 resize-none border-white/10 bg-white/10 px-3 py-2 text-sm leading-5 text-foreground ring-1 ring-white/10 placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring"
        onChange={(event) => onChange(event.target.value)}
      />
      {helpText ? <p className="mt-1.5 text-xs leading-4 text-muted-foreground">{helpText}</p> : null}
    </div>
  );
}

export function DeploymentSelectField({
  label,
  value,
  placeholder,
  options,
  optional = false,
  manageHref,
  manageLabel,
  onChange,
}: {
  label: string;
  value: string;
  placeholder: string;
  options: EntityOption[];
  optional?: boolean;
  manageHref?: string;
  manageLabel?: string;
  onChange: (value: string) => void;
}) {
  const id = `deployment-select-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
  const selected = options.find((option) => option.id === value);
  const items = [
    { value: '', label: placeholder },
    ...options.map((option) => ({ value: option.id, label: option.label })),
  ];
  return (
    <div>
      <DeploymentFieldHeader
        id={id}
        label={label}
        optional={optional}
        manageHref={manageHref}
        manageLabel={manageLabel}
      />
      <Select<string>
        value={value}
        items={items}
        onValueChange={(nextValue) => {
          if (nextValue !== null) {
            onChange(nextValue);
          }
        }}
      >
        <SelectTrigger
          id={id}
          className="h-8 w-full border-0 bg-white/10 px-3 text-sm ring-1 ring-white/10 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring"
        >
          <SelectValue className={value ? 'text-foreground' : 'text-muted-foreground'}>
            {selected?.label ?? placeholder}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value="" label={placeholder}>
            {placeholder}
          </SelectItem>
          {options.map((option) => (
            <SelectItem key={option.id} value={option.id} label={option.label}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

export function DeploymentAddSelectField({
  label,
  optional = false,
  valueLabel,
  selectedIds,
  options,
  manageHref,
  manageLabel,
  onChange,
}: {
  label: string;
  optional?: boolean;
  valueLabel: string;
  selectedIds: string[];
  options: EntityOption[];
  manageHref: string;
  manageLabel: string;
  onChange: (value: string[]) => void;
}) {
  const { msg } = useI18n();
  const id = `deployment-select-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
  const availableOptions = options.filter((option) => !selectedIds.includes(option.id));
  const selectedOptions = options.filter((option) => selectedIds.includes(option.id));
  const placeholder = availableOptions.length
    ? msg('managedAgents.common.addValue', 'Add {label}', { label: valueLabel })
    : msg('managedAgents.common.noValuesAvailable', 'No {label}s available', { label: valueLabel });
  const items = [
    { value: '', label: placeholder },
    ...availableOptions.map((option) => ({ value: option.id, label: option.label })),
  ];

  return (
    <div>
      <DeploymentFieldHeader
        id={id}
        label={label}
        optional={optional}
        manageHref={manageHref}
        manageLabel={manageLabel}
      />
      <Select<string>
        value=""
        items={items}
        disabled={!availableOptions.length}
        onValueChange={(nextId) => {
          if (nextId) {
            onChange([...selectedIds, nextId]);
          }
        }}
      >
        <SelectTrigger
          id={id}
          className="h-8 w-full border-0 bg-white/10 pl-3 pr-2 text-sm text-muted-foreground ring-1 ring-white/10 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:text-muted-foreground"
        >
          <Plus className="size-4 text-muted-foreground" aria-hidden />
          <SelectValue className="text-muted-foreground">{placeholder}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value="" label={placeholder} disabled>
            {placeholder}
          </SelectItem>
          {availableOptions.map((option) => (
            <SelectItem key={option.id} value={option.id} label={option.label}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {selectedOptions.length ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {selectedOptions.map((option) => (
            <Button
              key={option.id}
              type="button"
              variant="outline"
              size="xs"
              className="h-7 bg-white/10 text-xs text-foreground ring-1 ring-white/10 hover:bg-white/15"
              onClick={() => onChange(selectedIds.filter((idValue) => idValue !== option.id))}
            >
              {option.label}
              <X className="size-3" aria-hidden />
            </Button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function ManagedTextField({
  label,
  value,
  placeholder,
  disabled = false,
  autoFocus = false,
  onChange,
}: {
  label: string;
  value: string;
  placeholder?: string;
  disabled?: boolean;
  autoFocus?: boolean;
  onChange: (value: string) => void;
}) {
  const id = `managed-field-${useId()}`;
  return (
    <div>
      <Label htmlFor={id} className="text-sm font-medium text-foreground">
        {label}
      </Label>
      <Input
        id={id}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoFocus={autoFocus}
        className="managed-resource-field mt-2 h-10 border-border bg-secondary px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-ring focus:shadow-none focus-visible:shadow-none focus-visible:ring-0 disabled:cursor-not-allowed disabled:text-muted-foreground"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

export function ManagedTextArea({
  label,
  value,
  placeholder,
  onChange,
}: {
  label: string;
  value: string;
  placeholder?: string;
  onChange: (value: string) => void;
}) {
  const id = `managed-field-${useId()}`;
  return (
    <div>
      <Label htmlFor={id} className="text-sm font-medium text-foreground">
        {label}
      </Label>
      <Textarea
        id={id}
        value={value}
        rows={3}
        placeholder={placeholder}
        className="managed-resource-field mt-2 resize-none border-border bg-secondary px-3 py-2 text-sm leading-5 text-foreground placeholder:text-muted-foreground focus:border-ring focus:shadow-none focus-visible:shadow-none focus-visible:ring-0"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

export function ManagedSelectField({
  label,
  value,
  placeholder,
  options,
  onChange,
}: {
  label: string;
  value: string;
  placeholder: string;
  options: EntityOption[];
  onChange: (value: string) => void;
}) {
  const id = `managed-select-${useId()}`;
  const selected = options.find((option) => option.id === value);
  const items = [
    { value: '', label: placeholder },
    ...options.map((option) => ({ value: option.id, label: option.label })),
  ];
  return (
    <div>
      <Label htmlFor={id} className="text-sm font-medium text-foreground">
        {label}
      </Label>
      <Select<string>
        value={value}
        items={items}
        onValueChange={(nextValue) => {
          if (nextValue !== null) {
            onChange(nextValue);
          }
        }}
      >
        <SelectTrigger
          id={id}
          className="managed-resource-field mt-2 h-10 w-full border-border bg-secondary px-3 text-sm text-foreground focus-visible:border-ring focus-visible:ring-0"
        >
          <SelectValue className={value ? 'text-foreground' : 'text-muted-foreground'}>
            {selected?.label ?? placeholder}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value="" label={placeholder}>
            {placeholder}
          </SelectItem>
          {options.map((option) => (
            <SelectItem key={option.id} value={option.id} label={option.label}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

export function VaultMultiSelect({
  vaults,
  selectedIds,
  onChange,
}: {
  vaults: EntityOption[];
  selectedIds: string[];
  onChange: (ids: string[]) => void;
}) {
  const { msg } = useI18n();
  const toggle = (id: string) => {
    if (selectedIds.includes(id)) {
      onChange(selectedIds.filter((item) => item !== id));
    } else {
      onChange([...selectedIds, id]);
    }
  };

  return (
    <div>
      <div className="text-sm font-medium text-foreground">
        {msg('managedAgents.credentialVaults.title', 'Credential vaults')}
      </div>
      <div className="mt-2 rounded-lg border border-border bg-secondary p-2">
        {vaults.length ? (
          vaults.map((vault) => {
            const selected = selectedIds.includes(vault.id);
            const checkboxId = `vault-option-${vault.id}`;
            return (
              <Label
                key={vault.id}
                htmlFor={checkboxId}
                className="flex h-9 w-full cursor-pointer items-center gap-3 rounded-md px-2 text-left text-sm font-normal text-foreground transition hover:bg-accent"
              >
                <Checkbox
                  id={checkboxId}
                  checked={selected}
                  className="size-5 rounded-[5px] border-border data-checked:border-primary data-checked:bg-primary data-checked:text-primary-foreground"
                  onCheckedChange={() => toggle(vault.id)}
                />
                <span className="truncate">{vault.label}</span>
              </Label>
            );
          })
        ) : (
          <div className="px-2 py-2 text-sm text-muted-foreground">
            {msg('managedAgents.credentialVaults.selectOneOrMore', 'Select one or more vaults')}
          </div>
        )}
      </div>
    </div>
  );
}

export function ConfirmEntityDialog({
  action,
  section,
  entity,
  labelOverride,
  busy,
  onCancel,
  onConfirm,
}: {
  action: 'archive' | 'delete';
  section: ManagedEntitySection;
  entity: ManagedEntityApiResponse | VaultCredentialApiResponse | MemoryApiResponse;
  labelOverride?: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const label = labelOverride ?? entityKindLabel(section, msg);
  const actionLabel = action === 'archive' ? msg('common.archive', 'Archive') : msg('common.delete', 'Delete');
  const destructive = action === 'delete';
  const icon = destructive ? <Trash2 className="size-5" aria-hidden /> : <Archive className="size-5" aria-hidden />;
  const entityName =
    'display_name' in entity && typeof entity.display_name === 'string'
      ? entity.display_name
      : 'path' in entity && typeof entity.path === 'string'
        ? entity.path
        : entityDisplayName(section, entity as ManagedEntityApiResponse);

  return (
    <AlertDialog open onOpenChange={(open) => !open && onCancel()}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogMedia
            className={destructive ? 'bg-destructive/10 text-destructive dark:bg-destructive/20' : undefined}
          >
            {icon}
          </AlertDialogMedia>
          <AlertDialogTitle className="text-[20px] font-semibold text-foreground">
            {msg('managedAgents.common.confirmEntityTitle', '{action} {label}?', { action: actionLabel, label })}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-sm leading-5 text-muted-foreground">
            {action === 'archive'
              ? msg('managedAgents.common.confirmArchiveBody', '{name} will be hidden from active lists.', {
                  name: entityName,
                })
              : msg(
                  'managedAgents.common.confirmDeleteBody',
                  '{name} will be permanently removed from this workspace.',
                  { name: entityName },
                )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction
            type="button"
            disabled={busy}
            variant={destructive ? 'destructive' : 'default'}
            className="disabled:cursor-wait"
            onClick={onConfirm}
          >
            {busy ? msg('managedAgents.common.working', 'Working...') : actionLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function ConfirmAgentsArchiveDialog({
  count,
  busy,
  onCancel,
  onConfirm,
}: {
  count: number;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const single = count === 1;
  const title = single
    ? msg('managedAgents.agents.confirmArchive.title', 'Archive agent')
    : msg('managedAgents.agents.confirmArchive.titlePlural', 'Archive agents');
  const body = single
    ? msg(
        'managedAgents.agents.confirmArchive.body',
        'This agent will be hidden from the default view. Sessions that reference it keep working.',
      )
    : msg(
        'managedAgents.agents.confirmArchive.bodyPlural',
        '{count} agents will be hidden from the default view. Sessions that reference them keep working.',
        { count },
      );
  return (
    <AlertDialog open onOpenChange={(open) => !open && onCancel()}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <Archive className="size-5" aria-hidden />
          </AlertDialogMedia>
          <AlertDialogTitle className="text-[20px] font-semibold text-foreground">{title}</AlertDialogTitle>
          <AlertDialogDescription className="text-sm leading-5 text-muted-foreground">{body}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction
            type="button"
            disabled={busy}
            variant="destructive"
            className="disabled:cursor-wait"
            onClick={onConfirm}
          >
            {busy ? msg('managedAgents.agents.confirmArchive.busy', 'Archiving...') : msg('common.archive', 'Archive')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function AgentSelectionCheckbox({
  checked,
  indeterminate = false,
  disabled = false,
  label,
  onClick,
}: {
  checked: boolean;
  indeterminate?: boolean;
  disabled?: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Checkbox
      aria-label={label}
      checked={checked}
      indeterminate={indeterminate}
      disabled={disabled}
      className={clsx(
        'size-5 rounded-[5px] border-border bg-transparent hover:border-border',
        'data-checked:border-primary data-checked:bg-primary data-checked:text-primary-foreground',
        'data-indeterminate:border-primary data-indeterminate:bg-primary data-indeterminate:text-primary-foreground',
        disabled && 'cursor-not-allowed opacity-40',
      )}
      onCheckedChange={() => onClick()}
    />
  );
}

export function AgentsListState({
  icon: Icon,
  title,
  body,
  actionLabel,
  onAction,
}: {
  icon: IconComponent;
  title: string;
  body: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="grid min-h-[320px] place-items-center text-center">
      <div className="max-w-[360px]">
        <Icon className="mx-auto mb-4 size-12 stroke-[1.3] text-foreground" aria-hidden />
        <div className="text-sm font-semibold text-foreground">{title}</div>
        <p className="mt-3 text-sm leading-5 text-muted-foreground">{body}</p>
        {actionLabel && onAction ? (
          <Button type="button" variant="outline" className="mt-4" onClick={onAction}>
            {actionLabel}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

export function AgentsEmptyState({
  trueEmpty,
  truncated,
  trueEmptyActionLabel,
  onCreate,
  onReset,
}: {
  trueEmpty: boolean;
  truncated?: boolean;
  trueEmptyActionLabel: string;
  onCreate: () => void;
  onReset: () => void;
}) {
  const { msg } = useI18n();
  return (
    <AgentsListState
      icon={trueEmpty ? Bot : Search}
      title={
        trueEmpty
          ? msg('managedAgents.agents.emptyTitle', 'No agents yet')
          : msg('managedAgents.agents.noFilteredResults', 'No matching agents')
      }
      body={
        truncated
          ? msg(
              'managedAgents.agents.searchTruncatedEmptyBody',
              "Couldn't search every agent. Narrow the search or paste an exact ID.",
            )
          : trueEmpty
            ? msg('managedAgents.agents.emptyBody', 'Create an agent to start building managed workflows.')
            : msg('managedAgents.agents.noFilteredResultsBody', 'Try a different search or reset the filters.')
      }
      actionLabel={
        truncated
          ? undefined
          : trueEmpty
            ? trueEmptyActionLabel
            : msg('managedAgents.filters.resetFilters', 'Reset filters')
      }
      onAction={truncated ? undefined : trueEmpty ? onCreate : onReset}
    />
  );
}

export function EmptyState({ config }: { config: ResourceConfig }) {
  const { msg } = useI18n();
  const Icon = config.emptyIcon;
  const title = resourceEmptyTitle(config, msg);
  const body = resourceEmptyBody(config, msg);
  const action = resourceEmptyAction(config, msg);
  return (
    <div className="grid min-h-[320px] place-items-center text-center">
      <div>
        <Icon className="mx-auto mb-4 size-14 stroke-[1.2] text-foreground" aria-hidden />
        <div className="text-sm font-medium text-foreground">{title}</div>
        {body ? <p className="mt-3 text-sm text-muted-foreground">{body}</p> : null}
        {action ? (
          <Button type="button" variant="outline" className="mt-4">
            {action}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

export function CreateResourceDialog({ title, onClose }: { title: string; onClose: () => void }) {
  const { msg } = useI18n();
  const [name, setName] = useState('');

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (name.trim()) {
      onClose();
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent aria-label={title} className="sm:max-w-[440px]" showCloseButton={false}>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold">{title}</DialogTitle>
            <DialogDescription>
              {msg(
                'managedAgents.common.localMockNotice',
                'This local console mock keeps creation client-side for now.',
              )}
            </DialogDescription>
          </DialogHeader>

          <div>
            <Label htmlFor="managed-resource-name">{msg('common.name', 'Name')}</Label>
            <Input
              id="managed-resource-name"
              value={name}
              placeholder={msg('managedAgents.common.namePlaceholder', 'Enter a name')}
              className="mt-2 h-10 border-border bg-secondary placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-0"
              onChange={(event) => setName(event.target.value)}
              autoFocus
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={!name.trim()}>
              {msg('common.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function AgentStatusBadge({ archived }: { archived: boolean }) {
  const { msg } = useI18n();
  return (
    <Badge
      variant="secondary"
      className={clsx(
        'h-6 rounded-md px-2 text-xs font-medium',
        archived
          ? 'bg-secondary text-secondary-foreground'
          : 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
      )}
    >
      {archived ? msg('common.archived', 'Archived') : msg('common.active', 'Active')}
    </Badge>
  );
}

export function StatusPill({ children }: { children: ReactNode }) {
  return (
    <Badge variant="secondary" className="h-6 rounded-md px-2 text-xs font-medium text-secondary-foreground">
      {children}
    </Badge>
  );
}

export function CompactChip({ icon: Icon, children }: { icon: IconComponent; children: ReactNode }) {
  return (
    <Badge
      variant="outline"
      className="h-auto max-w-full items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-xs font-medium text-foreground"
    >
      <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="truncate">{children}</span>
    </Badge>
  );
}
