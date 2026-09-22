import { ArrowUpRight, Cloud, Globe, LockKeyhole, Monitor, Plus, Trash2 } from 'lucide-react';
import type { ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Input } from '../../../shared/ui/input';
import { Textarea } from '../../../shared/ui/textarea';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Field, FieldLabel, FieldError } from '../../../shared/ui/field';
import type { EnvironmentValidationErrors } from '../resources/environment-model';
import { parseAllowedHostsText } from '../resources/model';
import type { EnvironmentFormValues } from './model';

export type EnvironmentFieldsProps = {
  values: EnvironmentFormValues;
  onChange: (values: EnvironmentFormValues) => void;
  errors: EnvironmentValidationErrors;
  readOnly?: boolean;
};

export const environmentDocs = 'https://platform.claude.com/docs/en/managed-agents/environments';

export function EnvironmentSection({
  title,
  status,
  description,
  anchor,
  children,
  readOnly,
}: {
  title: string;
  status?: ReactNode;
  description: string;
  anchor?: string;
  children: ReactNode;
  readOnly?: boolean;
}) {
  const { msg } = useI18n();
  return (
    <section className="grid gap-5 border-b border-border py-8 first:pt-0 last:border-0 sm:grid-cols-[220px_minmax(0,1fr)] sm:gap-8">
      <div>
        <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1">
          <h2 className="text-sm font-semibold">{title}</h2>
          {status}
        </div>
        <p className="text-sm leading-relaxed text-pretty text-muted-foreground">
          {description}
          {anchor !== undefined && !readOnly ? (
            <>
              {' '}
              <a
                className="whitespace-nowrap text-foreground/75 underline underline-offset-4"
                href={`${environmentDocs}${anchor}`}
                target="_blank"
                rel="noreferrer"
              >
                {msg('common.learnMore', 'Learn more')} <ArrowUpRight className="inline size-3.5" aria-hidden />
              </a>
            </>
          ) : null}
        </p>
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
}

export function EnvironmentGeneral({
  values,
  onChange,
  errors,
  readOnly,
  creating,
}: EnvironmentFieldsProps & { creating: boolean }) {
  const { msg } = useI18n();
  const cloud = msg('managedAgents.environments.cloud', 'Cloud');
  const selfHosted = msg('managedAgents.environments.selfHosted', 'Self-hosted');
  const HostingIcon = values.hosting === 'cloud' ? Cloud : Monitor;
  return (
    <EnvironmentSection
      title={msg('environmentPage.general', 'General')}
      description={msg('environmentPage.generalDescription', 'What this environment is, and where it runs.')}
      anchor=""
      readOnly={readOnly}
    >
      <div className="space-y-4">
        <div className="space-y-2">
          <div className="text-sm font-normal">{msg('environmentPage.hosting', 'Hosting')}</div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm leading-5">
            <span className="inline-flex items-center gap-2">
              <HostingIcon className="size-4" strokeWidth={1.5} aria-hidden />
              {values.hosting === 'cloud' ? cloud : selfHosted}
            </span>
            <span className="text-muted-foreground">
              {msg('environmentPage.immutableHosting', 'Can’t be changed after creation')}
            </span>
          </div>
        </div>
        <Field className="gap-1.5" data-invalid={Boolean(errors.name)}>
          <FieldLabel htmlFor="environment-name">{msg('common.name', 'Name')}</FieldLabel>
          <Input
            id="environment-name"
            autoFocus={creating}
            readOnly={readOnly}
            className="read-only:bg-muted/50"
            placeholder={msg('environmentPage.namePlaceholder', 'E.g. data-analysis')}
            value={values.name}
            aria-invalid={Boolean(errors.name)}
            onChange={(event) => onChange({ ...values, name: event.target.value })}
          />
          {errors.name ? <FieldError>{errors.name}</FieldError> : null}
        </Field>
        <Field className="gap-1.5">
          <FieldLabel htmlFor="environment-description">
            {msg('common.description', 'Description')}{' '}
            {!readOnly ? (
              <span className="font-normal text-muted-foreground">{msg('environmentPage.optional', '(optional)')}</span>
            ) : null}
          </FieldLabel>
          <Textarea
            id="environment-description"
            readOnly={readOnly}
            className="min-h-20 resize-y read-only:resize-none read-only:bg-muted/50"
            rows={2}
            value={values.description}
            placeholder={msg('environmentPage.descriptionPlaceholder', 'What sessions in this environment are for')}
            onChange={(event) => onChange({ ...values, description: event.target.value })}
          />
        </Field>
      </div>
    </EnvironmentSection>
  );
}

export function EnvironmentNetworking({ values, onChange, readOnly }: EnvironmentFieldsProps) {
  const { msg } = useI18n();
  const switches = [
    {
      field: 'allowPackageManagers',
      label: msg('environmentPage.allowPackages', 'Allow package managers'),
      description: msg(
        'environmentPage.allowPackagesDescription',
        'Reach PyPI, npm, and the other package registries.',
      ),
    },
    {
      field: 'allowMcpServers',
      label: msg('environmentPage.allowMcp', 'Allow MCP servers'),
      description: msg('environmentPage.allowMcpDescription', 'Reach the MCP servers configured on the agent.'),
    },
  ] as const;
  return (
    <EnvironmentSection
      title={msg('managedAgents.environments.networking.title', 'Networking')}
      description={msg('environmentPage.networkingDescription', 'What the container can reach over the network.')}
      anchor="#networking"
      readOnly={readOnly}
    >
      <div className="space-y-3">
        <RadioGroup
          aria-label={msg('environmentPage.networkAccess', 'Network access')}
          value={values.networkType}
          disabled={readOnly}
          onValueChange={(networkType) =>
            onChange({ ...values, networkType: String(networkType) as EnvironmentFormValues['networkType'] })
          }
          className="grid-cols-2 gap-0 rounded-lg bg-muted p-0.5"
        >
          {[
            {
              value: 'unrestricted',
              label: msg('managedAgents.environments.networking.unrestricted', 'Unrestricted'),
              icon: Globe,
            },
            {
              value: 'limited',
              label: msg('managedAgents.environments.networking.limited', 'Limited'),
              icon: LockKeyhole,
            },
          ].map(({ value, label, icon: Icon }) => (
            <RadioGroupItem
              key={value}
              value={value}
              className="h-7 w-full aspect-auto items-center justify-center gap-2 rounded-md border-transparent bg-transparent text-sm text-muted-foreground after:hidden data-checked:border-border data-checked:bg-background data-checked:text-foreground data-checked:shadow-sm [&>[data-slot=radio-group-indicator]]:hidden"
            >
              <Icon className="size-4" strokeWidth={1.5} aria-hidden />
              {label}
            </RadioGroupItem>
          ))}
        </RadioGroup>
        <p className="text-xs leading-5 text-muted-foreground">
          {values.networkType === 'unrestricted'
            ? msg('environmentPage.unrestrictedHelp', 'Sessions can reach any host on the internet.')
            : msg('environmentPage.limitedHelp', 'Outbound traffic is blocked except for what’s allowed below.')}
        </p>
        {values.networkType === 'limited' ? (
          <>
            <div className="space-y-3">
              {switches.map(({ field, label, description }) => (
                <label key={field} className="flex items-start gap-2 text-sm">
                  <Checkbox
                    aria-label={label}
                    className="mt-0.5"
                    checked={values[field]}
                    disabled={readOnly}
                    onCheckedChange={(checked) => onChange({ ...values, [field]: checked })}
                  />
                  <span className={readOnly ? 'text-muted-foreground' : ''}>
                    {label}
                    <span className="block text-xs leading-5 text-muted-foreground">{description}</span>
                  </span>
                </label>
              ))}
            </div>
            <Field className="gap-1.5 pt-1">
              <div className="flex justify-between">
                <FieldLabel htmlFor="environment-hosts">
                  {msg('managedAgents.environments.networking.allowedHosts', 'Allowed hosts')}
                  {!readOnly ? (
                    <span className="font-normal text-muted-foreground">
                      {msg('environmentPage.optional', '(optional)')}
                    </span>
                  ) : null}
                </FieldLabel>
                {!readOnly ? (
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {parseAllowedHostsText(values.allowedHostsText).length}/16
                  </span>
                ) : null}
              </div>
              {readOnly ? (
                <p className="whitespace-pre-wrap text-sm text-muted-foreground">
                  {values.allowedHostsText || msg('environmentPage.noHosts', 'No allowed hosts.')}
                </p>
              ) : (
                <>
                  <Textarea
                    id="environment-hosts"
                    rows={2}
                    className="min-h-24 resize-y font-mono"
                    value={values.allowedHostsText}
                    placeholder={'api.example.com\n*.internal.example.com'}
                    onChange={(event) => onChange({ ...values, allowedHostsText: event.target.value })}
                  />
                  <p className="text-xs leading-5 text-muted-foreground">
                    {msg(
                      'environmentPage.hostHelp',
                      'Hostnames or IPv4 addresses, one per line. *.example.com covers subdomains.',
                    )}
                  </p>
                </>
              )}
            </Field>
          </>
        ) : null}
      </div>
    </EnvironmentSection>
  );
}

export function EnvironmentMetadata({ values, onChange, errors, readOnly }: EnvironmentFieldsProps) {
  const { msg } = useI18n();
  const update = (index: number, field: 'key' | 'value', value: string) =>
    onChange({
      ...values,
      metadataRows: values.metadataRows.map((row, position) => (position === index ? { ...row, [field]: value } : row)),
    });
  return (
    <EnvironmentSection
      title={msg('managedAgents.environments.metadata.title', 'Metadata')}
      description={msg(
        'environmentPage.metadataDescription',
        'Key-value pairs for your own bookkeeping, not shown to the agent.',
      )}
    >
      <div className="space-y-3">
        {values.metadataRows.map((row, index) => (
          <div key={index}>
            <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_36px] gap-2">
              <Input
                aria-label={msg('environmentPage.metadataKey', 'Metadata key {number}', { number: index + 1 })}
                autoFocus={!row.key && index === values.metadataRows.length - 1}
                readOnly={readOnly}
                placeholder={msg('common.key', 'Key')}
                className="read-only:bg-muted/50"
                value={row.key}
                aria-invalid={Boolean(errors.metadataRows[index]?.key)}
                onChange={(event) => update(index, 'key', event.target.value)}
              />
              <Input
                aria-label={msg('environmentPage.metadataValue', 'Metadata value {number}', { number: index + 1 })}
                readOnly={readOnly}
                placeholder={msg('common.value', 'Value')}
                className="read-only:bg-muted/50"
                value={row.value}
                aria-invalid={Boolean(errors.metadataRows[index]?.value)}
                onChange={(event) => update(index, 'value', event.target.value)}
              />
              {!readOnly ? (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={msg('environmentPage.removeMetadata', 'Remove {key}', {
                    key: row.key || msg('environmentPage.emptyEntry', 'empty entry'),
                  })}
                  onClick={() =>
                    onChange({
                      ...values,
                      metadataRows: values.metadataRows.filter((_, position) => index !== position),
                    })
                  }
                >
                  <Trash2 className="size-4" strokeWidth={1.5} />
                </Button>
              ) : null}
            </div>
            {errors.metadataRows[index] ? (
              <FieldError>{errors.metadataRows[index].key || errors.metadataRows[index].value}</FieldError>
            ) : null}
          </div>
        ))}
        {readOnly && !values.metadataRows.length ? (
          <p className="text-sm text-muted-foreground">{msg('environmentPage.noMetadata', 'No metadata.')}</p>
        ) : null}
        {!readOnly ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={values.metadataRows.length >= 16}
            onClick={() => onChange({ ...values, metadataRows: [...values.metadataRows, { key: '', value: '' }] })}
          >
            <Plus className="size-4" strokeWidth={1.5} />
            {msg('managedAgents.environments.metadata.add', 'Add metadata')}
          </Button>
        ) : null}
        {errors.metadata ? <FieldError>{errors.metadata}</FieldError> : null}
      </div>
    </EnvironmentSection>
  );
}
