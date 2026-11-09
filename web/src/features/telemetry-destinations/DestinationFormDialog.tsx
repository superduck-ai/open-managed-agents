import { Building2, ChevronDown, Info, Link2, List, Loader2, Minus, Plus, Trash2 } from 'lucide-react';
import { useState, type FormEvent, type ReactNode } from 'react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../shared/ui/collapsible';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Field, FieldError, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import { RadioGroup, RadioGroupItem } from '../../shared/ui/radio-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Switch } from '../../shared/ui/switch';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../shared/ui/tooltip';
import { useWorkspace } from '../../shared/workspaces/context';
import type { ContentMode, DestinationInput, WorkspaceScope } from './api';
import { requestErrorMessage } from './errors';
import {
  DEFAULT_MAX_CONTENT_MB,
  formErrors,
  hasErrors,
  inputFromForm,
  type DestinationForm,
  type HeaderRow,
} from './form-model';

type Msg = ReturnType<typeof useI18n>['msg'];
type FieldsProps = { form: DestinationForm; onChange: (form: DestinationForm) => void; disabled: boolean };

const SCOPE_ICONS = { all: Building2, include: List, exclude: Minus } as const;

function scopeLabel(scope: WorkspaceScope, msg: Msg) {
  if (scope === 'include') return msg('telemetry.scope.include', 'Selected');
  if (scope === 'exclude') return msg('telemetry.scope.exclude', 'All except selected');
  return msg('telemetry.scope.all', 'All');
}

function FieldHeading({
  htmlFor,
  children,
  optional,
  hint,
}: {
  htmlFor?: string;
  children: ReactNode;
  optional?: boolean;
  hint?: string;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex items-center gap-1.5">
      <FieldLabel htmlFor={htmlFor}>
        {children}
        {optional ? (
          <span className="font-normal text-muted-foreground">{msg('telemetry.optional', '(optional)')}</span>
        ) : null}
      </FieldLabel>
      {hint ? (
        <Tooltip>
          <TooltipTrigger type="button" className="cursor-help text-muted-foreground">
            <Info className="size-3.5" aria-hidden />
            <span className="sr-only">{hint}</span>
          </TooltipTrigger>
          <TooltipContent className="max-w-80">{hint}</TooltipContent>
        </Tooltip>
      ) : null}
    </div>
  );
}

function ProviderAndName({ form, onChange, disabled }: FieldsProps) {
  const { msg } = useI18n();
  return (
    <div className="grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)] gap-3">
      <Field>
        <FieldLabel>{msg('telemetry.provider', 'Provider')}</FieldLabel>
        <Select value="custom">
          <SelectTrigger aria-label={msg('telemetry.provider', 'Provider')} disabled={disabled}>
            <SelectValue>
              <Link2 className="size-4" aria-hidden />
              {msg('telemetry.provider.custom', 'Custom')}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="custom" label={msg('telemetry.provider.custom', 'Custom')}>
              {msg('telemetry.provider.custom', 'Custom')}
            </SelectItem>
          </SelectContent>
        </Select>
      </Field>
      <Field>
        <FieldHeading htmlFor="telemetry-name" optional>
          {msg('telemetry.displayName', 'Name')}
        </FieldHeading>
        <Input
          id="telemetry-name"
          value={form.displayName}
          disabled={disabled}
          onChange={(event) => onChange({ ...form, displayName: event.target.value })}
        />
      </Field>
    </div>
  );
}

function HeaderRows({
  rows,
  onChange,
  disabled,
}: {
  rows: HeaderRow[];
  onChange: (rows: HeaderRow[]) => void;
  disabled: boolean;
}) {
  const { msg } = useI18n();
  const update = (index: number, patch: Partial<HeaderRow>) =>
    onChange(rows.map((row, current) => (current === index ? { ...row, ...patch } : row)));
  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, index) => (
        <div key={index} className="flex items-center gap-2">
          <Input
            aria-label={msg('telemetry.headerName', 'Header name')}
            placeholder={msg('telemetry.headerNamePlaceholder', 'Header name (e.g. x-api-key)')}
            value={row.name}
            disabled={disabled}
            onChange={(event) => update(index, { name: event.target.value })}
          />
          <Input
            aria-label={msg('telemetry.headerValue', 'Header value')}
            placeholder={msg('telemetry.headerValuePlaceholder', 'Value')}
            type="password"
            autoComplete="off"
            value={row.value}
            disabled={disabled}
            onChange={(event) => update(index, { value: event.target.value })}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('telemetry.removeHeader', 'Remove header')}
            disabled={disabled}
            onClick={() => onChange(rows.filter((_, current) => current !== index))}
          >
            <Trash2 />
          </Button>
        </div>
      ))}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="self-start"
        disabled={disabled || rows.length >= 20}
        onClick={() => onChange([...rows, { name: '', value: '' }])}
      >
        <Plus />
        {msg('telemetry.addHeader', 'Add header')}
      </Button>
    </div>
  );
}

function HeaderFields({
  form,
  onChange,
  disabled,
  editing,
  storedHeaderNames,
}: FieldsProps & { editing: boolean; storedHeaderNames: string[] }) {
  const { msg } = useI18n();
  return (
    <Field>
      <FieldHeading
        optional
        hint={msg(
          'telemetry.headersHint',
          'Headers sent with every request, such as your platform API key. Put credentials only in header values; they are encrypted and not shown again after saving.',
        )}
      >
        {msg('telemetry.headers', 'Request headers')}
      </FieldHeading>
      {editing ? (
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <Switch
            checked={form.replaceHeaders}
            disabled={disabled}
            onCheckedChange={(checked) =>
              onChange({ ...form, replaceHeaders: checked, headers: checked ? [{ name: '', value: '' }] : [] })
            }
          />
          {storedHeaderNames.length
            ? msg('telemetry.replaceStoredHeaders', 'Replace stored headers ({names})', {
                names: storedHeaderNames.join(', '),
              })
            : msg('telemetry.replaceHeaders', 'Replace stored headers')}
        </label>
      ) : null}
      {form.replaceHeaders ? (
        <HeaderRows rows={form.headers} disabled={disabled} onChange={(headers) => onChange({ ...form, headers })} />
      ) : null}
    </Field>
  );
}

function ContentModeField({ form, onChange, disabled }: FieldsProps) {
  const { msg } = useI18n();
  const options: { value: ContentMode; title: string; hint: string }[] = [
    {
      value: 'metadata_only',
      title: msg('telemetry.contentMode.metadata', 'Metadata only'),
      hint: msg(
        'telemetry.contentMode.metadataHint',
        'Spans contain identifiers, timing and status, never prompts or tool content.',
      ),
    },
    {
      value: 'content',
      title: msg('telemetry.contentMode.content', 'Full content'),
      hint: msg('telemetry.contentMode.contentHint', 'Spans also include message and tool content.'),
    },
  ];
  return (
    <Field>
      <FieldLabel>{msg('telemetry.contentModeLabel', 'Content mode')}</FieldLabel>
      <RadioGroup
        aria-label={msg('telemetry.contentModeLabel', 'Content mode')}
        value={form.contentMode}
        disabled={disabled}
        onValueChange={(value) => onChange({ ...form, contentMode: value as ContentMode })}
      >
        {options.map((option) => (
          <label
            key={option.value}
            className="flex cursor-pointer items-start gap-3 rounded-lg border border-border p-3 has-[[data-checked]]:border-primary/40 has-[[data-checked]]:bg-primary/5"
          >
            <RadioGroupItem value={option.value} className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">{option.title}</span>
              <span className="text-sm text-muted-foreground">{option.hint}</span>
            </span>
          </label>
        ))}
      </RadioGroup>
    </Field>
  );
}

function WorkspaceField({ form, onChange, disabled }: FieldsProps) {
  const { msg } = useI18n();
  const { workspaces } = useWorkspace();
  return (
    <Field>
      <FieldLabel>{msg('telemetry.workspaces', 'Workspaces')}</FieldLabel>
      <div>
        <RadioGroup
          aria-label={msg('telemetry.workspaces', 'Workspaces')}
          value={form.scope}
          disabled={disabled}
          onValueChange={(value) => onChange({ ...form, scope: value as WorkspaceScope, workspacesChanged: true })}
          className="flex w-fit gap-0.5 rounded-lg bg-muted p-0.5"
        >
          {(['all', 'include', 'exclude'] as const).map((scope) => {
            const Icon = SCOPE_ICONS[scope];
            return (
              <RadioGroupItem
                key={scope}
                value={scope}
                className="h-8 w-auto aspect-auto items-center justify-center gap-2 rounded-md border-transparent bg-transparent px-3 text-sm text-muted-foreground after:hidden data-checked:border-border data-checked:bg-background data-checked:text-foreground data-checked:shadow-sm dark:bg-transparent dark:data-checked:bg-background [&>[data-slot=radio-group-indicator]]:hidden"
              >
                <Icon className="size-4" strokeWidth={1.5} aria-hidden />
                {scopeLabel(scope, msg)}
              </RadioGroupItem>
            );
          })}
        </RadioGroup>
      </div>
      {form.scope !== 'all' ? (
        <div className="flex max-h-40 flex-col gap-2 overflow-y-auto rounded-md border border-border p-2">
          {workspaces.map((workspace) => (
            <label key={workspace.id} className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={form.workspaceIds.includes(workspace.id)}
                disabled={disabled}
                onCheckedChange={(checked) =>
                  onChange({
                    ...form,
                    workspacesChanged: true,
                    workspaceIds: checked
                      ? [...form.workspaceIds, workspace.id]
                      : form.workspaceIds.filter((id) => id !== workspace.id),
                  })
                }
              />
              <span className="truncate">{workspace.name}</span>
            </label>
          ))}
        </div>
      ) : null}
    </Field>
  );
}

function AdvancedFields({ form, onChange, disabled }: FieldsProps) {
  const { msg } = useI18n();
  const errors = formErrors(form);
  return (
    <Collapsible defaultOpen={Boolean(form.tracesUrl) || form.maxContentMb !== DEFAULT_MAX_CONTENT_MB}>
      <CollapsibleTrigger className="group flex items-center gap-1.5 text-sm font-medium">
        <ChevronDown className="size-4 transition-transform group-data-[panel-open]:rotate-180" aria-hidden />
        {msg('telemetry.advanced', 'Advanced settings')}
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-4 pt-4">
        <Field>
          <FieldHeading
            htmlFor="telemetry-traces-url"
            optional
            hint={msg(
              'telemetry.tracesUrlHint',
              'Full URL of your platform traces endpoint, when it receives traces somewhere other than the endpoint URL plus /v1/traces. It must use the same host.',
            )}
          >
            {msg('telemetry.tracesUrl', 'OTLP traces endpoint URL')}
          </FieldHeading>
          <Input
            id="telemetry-traces-url"
            placeholder="https://otlp.example.com/custom/traces"
            value={form.tracesUrl}
            disabled={disabled}
            onChange={(event) => onChange({ ...form, tracesUrl: event.target.value })}
          />
        </Field>
        <Field data-invalid={errors.maxContentMb || undefined}>
          <FieldHeading
            htmlFor="telemetry-max-content"
            optional
            hint={msg(
              'telemetry.maxContentHint',
              'Maximum message and tool content sent in each span when the content mode is Full content, up to 0.75 MB. Leave empty to use the maximum.',
            )}
          >
            {msg('telemetry.maxContentMb', 'Maximum content per span (MB)')}
          </FieldHeading>
          <Input
            id="telemetry-max-content"
            inputMode="decimal"
            placeholder={DEFAULT_MAX_CONTENT_MB}
            value={form.maxContentMb}
            disabled={disabled}
            aria-invalid={errors.maxContentMb || undefined}
            onChange={(event) => onChange({ ...form, maxContentMb: event.target.value })}
          />
          {errors.maxContentMb ? (
            <FieldError>
              {msg('telemetry.maxContentInvalid', 'Enter a size greater than 0 and at most 0.75 MB.')}
            </FieldError>
          ) : null}
        </Field>
      </CollapsibleContent>
    </Collapsible>
  );
}

export function DestinationFormDialog({
  open,
  title,
  initial,
  editing,
  storedHeaderNames = [],
  isSubmitting,
  onClose,
  onSubmit,
}: {
  open: boolean;
  title: string;
  initial: DestinationForm;
  editing: boolean;
  storedHeaderNames?: string[];
  isSubmitting: boolean;
  onClose: () => void;
  onSubmit: (input: DestinationInput) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [form, setForm] = useState(initial);
  const [error, setError] = useState('');
  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (hasErrors(form) || isSubmitting) return;
    setError('');
    try {
      await onSubmit(inputFromForm(form));
    } catch (submitError) {
      setError(requestErrorMessage(submitError, msg('telemetry.saveFailed', 'Could not save destination'), msg));
    }
  };
  const fields = { form, onChange: setForm, disabled: isSubmitting };
  const endpointInvalid = Boolean(form.endpointUrl.trim()) && formErrors(form).endpointUrl;
  return (
    <Dialog open={open} onOpenChange={(next) => !next && !isSubmitting && onClose()}>
      <DialogContent className="grid-rows-[auto_minmax(0,1fr)] max-h-[min(820px,calc(100vh-48px))] gap-0 overflow-hidden p-0 sm:max-w-[640px]">
        <DialogHeader className="px-5 pt-5 pb-2">
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {msg(
              'telemetry.dialogDescription',
              'After you add a telemetry destination, traces of agent activity in this organization are sent to the OTLP/HTTP endpoint you specify. Once delivered, the data is held by that service and subject to its retention and access controls.',
            )}
          </DialogDescription>
        </DialogHeader>
        <TooltipProvider>
          <form className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto]" onSubmit={handleSubmit}>
            <div className="subtle-scrollbar-auto flex min-h-0 flex-col gap-5 overflow-y-auto py-3 pr-3 pl-5">
              <ProviderAndName {...fields} />
              <Field data-invalid={endpointInvalid || undefined}>
                <FieldHeading
                  htmlFor="telemetry-endpoint"
                  hint={msg(
                    'telemetry.endpointHint',
                    'Base HTTPS URL of your platform OTLP/HTTP endpoint, using a hostname rather than an IP address. It can include a path prefix; do not add /v1/traces, it is appended automatically.',
                  )}
                >
                  {msg('telemetry.endpointUrl', 'OTLP endpoint URL')}
                </FieldHeading>
                <Input
                  id="telemetry-endpoint"
                  placeholder="https://otlp.example.com"
                  value={form.endpointUrl}
                  disabled={isSubmitting}
                  aria-invalid={endpointInvalid || undefined}
                  onChange={(event) => setForm({ ...form, endpointUrl: event.target.value })}
                />
                {endpointInvalid ? (
                  <FieldError>
                    {msg(
                      'telemetry.endpointInvalid',
                      'Enter a full URL starting with https://, for example https://otlp.example.com.',
                    )}
                  </FieldError>
                ) : null}
              </Field>
              <HeaderFields {...fields} editing={editing} storedHeaderNames={storedHeaderNames} />
              <ContentModeField {...fields} />
              <WorkspaceField {...fields} />
              <AdvancedFields {...fields} />
            </div>
            <div className="px-5">
              {error ? (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              ) : null}
              <DialogFooter className="py-4">
                <Button type="submit" size="lg" disabled={hasErrors(form) || isSubmitting}>
                  {isSubmitting ? <Loader2 className="animate-spin" aria-hidden /> : null}
                  {editing ? msg('common.save', 'Save') : msg('telemetry.submitCreate', 'Add destination')}
                </Button>
              </DialogFooter>
            </div>
          </form>
        </TooltipProvider>
      </DialogContent>
    </Dialog>
  );
}
