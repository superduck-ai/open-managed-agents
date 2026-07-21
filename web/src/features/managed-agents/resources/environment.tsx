import { AlertCircle, Plus, X } from 'lucide-react';
import { type FormEvent, useMemo, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { Card, CardContent } from '../../../shared/ui/card';
import { Field, FieldDescription, FieldError, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Switch } from '../../../shared/ui/switch';
import { Textarea } from '../../../shared/ui/textarea';
import { DetailCard, ManagedTextArea } from '../components/common';
import { type EnvironmentApiResponse, type EnvironmentEditValues, type ManagedEntityApiResponse } from '../types';
import { environmentEditValues } from './model';
import { updateEnvironmentDetail } from './environment-update';
import { UnsavedEnvironmentChangesDialog, useUnsavedChangesGuard } from './environment-form';
import {
  environmentErrorMessage,
  environmentFormFingerprint,
  hasEnvironmentValidationErrors,
  validateEnvironment,
} from './environment-model';

const packageManagers = ['apt', 'cargo', 'gem', 'go', 'npm', 'pip'];

export function EnvironmentInlineEditor({
  entity,
  workspaceId,
  onCancel,
  onSaved,
}: {
  entity: EnvironmentApiResponse;
  workspaceId: string;
  onCancel: () => void;
  onSaved: (entity: ManagedEntityApiResponse) => void;
}) {
  const { msg } = useI18n();
  const initialValues = useMemo(() => environmentEditValues(entity), [entity]);
  const initialFingerprint = useMemo(() => environmentFormFingerprint(initialValues), [initialValues]);
  const [values, setValues] = useState<EnvironmentEditValues>(initialValues);
  const [submitting, setSubmitting] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submittingRef = useRef(false);
  const dirty = environmentFormFingerprint(values) !== initialFingerprint;
  const validation = attempted ? validateEnvironment(values, msg, initialValues) : { packages: {}, metadataRows: {} };
  const guard = useUnsavedChangesGuard({ dirty, interactionBlocked: submitting, onDiscard: onCancel });

  const updateValues = (update: (current: EnvironmentEditValues) => EnvironmentEditValues) => {
    setValues(update);
    if (attempted) {
      setError(null);
    }
  };
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submittingRef.current) {
      return;
    }
    setAttempted(true);
    const nextValidation = validateEnvironment(values, msg, initialValues);
    if (hasEnvironmentValidationErrors(nextValidation)) {
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    setError(null);
    try {
      const updated = await updateEnvironmentDetail(entity.id, values, initialValues, workspaceId);
      onSaved(updated);
    } catch (submitError) {
      setError(environmentErrorMessage(submitError, 'update', msg));
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  return (
    <>
      <UnsavedEnvironmentChangesDialog
        open={guard.confirmOpen}
        onContinue={guard.continueEditing}
        onDiscard={guard.discard}
      />
      <form className="max-w-[820px] space-y-7" onSubmit={submit}>
        <Field data-invalid={Boolean(validation.name)}>
          <FieldLabel htmlFor="environment-name">
            {msg('managedAgents.environments.name', 'Environment name')}
          </FieldLabel>
          <Input
            id="environment-name"
            value={values.name}
            autoFocus
            aria-invalid={Boolean(validation.name) || undefined}
            className="mt-2 h-10"
            onChange={(event) => updateValues((current) => ({ ...current, name: event.target.value }))}
          />
          {validation.name ? <FieldError>{validation.name}</FieldError> : null}
        </Field>
        <ManagedTextArea
          label={msg('common.description', 'Description')}
          value={values.description}
          onChange={(description) => updateValues((current) => ({ ...current, description }))}
        />
        <EnvironmentNetworkingEditor values={values} onChange={updateValues} />
        <EnvironmentPackagesEditor values={values} errors={validation.packages} onChange={updateValues} />
        <EnvironmentMetadataEditor
          values={values}
          error={validation.metadata}
          rowErrors={validation.metadataRows}
          onChange={updateValues}
        />
        {error ? (
          <Alert variant="destructive">
            <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <div className="flex gap-2">
          <Button type="submit" size="lg" disabled={submitting}>
            {submitting ? msg('common.saving', 'Saving...') : msg('common.saveChanges', 'Save changes')}
          </Button>
          <Button type="button" variant="secondary" size="lg" disabled={submitting} onClick={guard.requestDiscard}>
            {msg('common.cancel', 'Cancel')}
          </Button>
        </div>
      </form>
    </>
  );
}

function EnvironmentNetworkingEditor({
  values,
  onChange,
}: {
  values: EnvironmentEditValues;
  onChange: (update: (current: EnvironmentEditValues) => EnvironmentEditValues) => void;
}) {
  const { msg } = useI18n();
  const unrestricted = msg('managedAgents.environments.networking.unrestricted', 'Unrestricted');
  const limited = msg('managedAgents.environments.networking.limited', 'Limited');
  return (
    <DetailCard
      title={msg('managedAgents.environments.networking.title', 'Networking')}
      description={msg(
        'managedAgents.environments.networking.description',
        'Configure network access policies for this environment.',
      )}
    >
      <Card size="sm" className="py-0">
        <CardContent className="space-y-6 p-6">
          <Field className="gap-2">
            <FieldLabel>{msg('managedAgents.environments.networking.type', 'Type')}</FieldLabel>
            <Select<string>
              value={values.networkType}
              items={[
                { value: 'unrestricted', label: unrestricted },
                { value: 'limited', label: limited },
              ]}
              onValueChange={(networkType) =>
                networkType &&
                onChange((current) => ({
                  ...current,
                  networkType: networkType === 'limited' ? 'limited' : 'unrestricted',
                }))
              }
            >
              <SelectTrigger
                aria-label={msg('managedAgents.environments.networking.type', 'Type')}
                className="h-10 w-full px-3 text-sm text-foreground"
              >
                <SelectValue>{values.networkType === 'limited' ? limited : unrestricted}</SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value="unrestricted" label={unrestricted}>
                  {unrestricted}
                </SelectItem>
                <SelectItem value="limited" label={limited}>
                  {limited}
                </SelectItem>
              </SelectContent>
            </Select>
            <FieldDescription>
              {msg(
                'managedAgents.environments.networking.typeDescription',
                'Unrestricted allows all outbound HTTPS. Limited only allows the hosts configured below.',
              )}
            </FieldDescription>
          </Field>
          {values.networkType === 'limited' ? (
            <div className="space-y-6 border-t border-border pt-6">
              <NetworkingSwitchRow
                label={msg('managedAgents.environments.networking.allowMcpServers', 'Allow MCP server network access')}
                description={msg(
                  'managedAgents.environments.networking.allowMcpServersDescription',
                  'Allow the hosts used by the MCP servers configured on this agent.',
                )}
                checked={values.allowMcpServers}
                onCheckedChange={(checked) => onChange((current) => ({ ...current, allowMcpServers: checked }))}
              />
              <NetworkingSwitchRow
                label={msg(
                  'managedAgents.environments.networking.allowPackageManagers',
                  'Allow package manager network access',
                )}
                description={msg(
                  'managedAgents.environments.networking.allowPackageManagersDescription',
                  'Allow official registries and trusted mirrors for apt, pip, npm, Go, Cargo, and RubyGems.',
                )}
                checked={values.allowPackageManagers}
                onCheckedChange={(checked) => onChange((current) => ({ ...current, allowPackageManagers: checked }))}
              />
              <Field className="gap-2">
                <FieldLabel>{msg('managedAgents.environments.networking.allowedHosts', 'Allowed hosts')}</FieldLabel>
                <Textarea
                  value={values.allowedHostsText}
                  aria-label={msg('managedAgents.environments.networking.allowedHosts', 'Allowed hosts')}
                  placeholder="api.example.com, *.example.com"
                  rows={4}
                  className="font-mono text-sm"
                  onChange={(event) => onChange((current) => ({ ...current, allowedHostsText: event.target.value }))}
                />
                <FieldDescription>
                  {msg(
                    'managedAgents.environments.networking.allowedHostsDescription',
                    'Separate hosts with commas or new lines. Use *.example.com to allow its subdomains.',
                  )}
                </FieldDescription>
              </Field>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </DetailCard>
  );
}

function NetworkingSwitchRow({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-6">
      <div className="space-y-1">
        <FieldLabel>{label}</FieldLabel>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <Switch
        checked={checked}
        aria-label={label}
        className="shrink-0"
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
    </div>
  );
}

function EnvironmentPackagesEditor({
  values,
  errors,
  onChange,
}: {
  values: EnvironmentEditValues;
  errors: Record<number, string>;
  onChange: (update: (current: EnvironmentEditValues) => EnvironmentEditValues) => void;
}) {
  const { msg } = useI18n();
  const updateRow = (index: number, patch: Partial<EnvironmentEditValues['packages'][number]>) =>
    onChange((current) => ({
      ...current,
      packages: current.packages.map((row, rowIndex) => (rowIndex === index ? { ...row, ...patch } : row)),
    }));
  return (
    <DetailCard
      title={msg('managedAgents.environments.packages.title', 'Packages')}
      description={msg(
        'managedAgents.environments.packages.description',
        'Specify packages and versions available in this environment. Separate multiple values with spaces.',
      )}
    >
      <Card size="sm" className="py-0">
        <CardContent className="space-y-3 p-3">
          {values.packages.map((row, index) => (
            <div key={index} className="grid gap-2 md:grid-cols-[160px_1fr_40px]">
              <Select<string>
                value={row.manager}
                items={packageManagers.map((manager) => ({ value: manager, label: manager }))}
                onValueChange={(manager) => manager && updateRow(index, { manager })}
              >
                <SelectTrigger
                  aria-label={msg('managedAgents.environments.packages.manager', 'Package manager')}
                  className="h-10 w-full px-3 text-sm text-foreground"
                >
                  <SelectValue>{row.manager}</SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {packageManagers.map((manager) => (
                    <SelectItem key={manager} value={manager} label={manager}>
                      {manager}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Field data-invalid={Boolean(errors[index])}>
                <Input
                  aria-label={msg('managedAgents.environments.packages.valueAria', 'Package value {number}', {
                    number: index + 1,
                  })}
                  value={row.value}
                  placeholder="package==1.0.0"
                  aria-invalid={Boolean(errors[index]) || undefined}
                  className="h-10"
                  onChange={(event) => updateRow(index, { value: event.target.value })}
                />
                {errors[index] ? <FieldError>{errors[index]}</FieldError> : null}
              </Field>
              <Button
                type="button"
                aria-label={msg('managedAgents.environments.packages.remove', 'Remove package')}
                disabled={values.packages.length <= 1}
                variant="secondary"
                size="icon-lg"
                className="size-10"
                onClick={() =>
                  onChange((current) => ({
                    ...current,
                    packages: current.packages.filter((_, rowIndex) => rowIndex !== index),
                  }))
                }
              >
                <X className="size-4" aria-hidden />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="secondary"
            size="lg"
            onClick={() =>
              onChange((current) => ({
                ...current,
                packages: [...current.packages, { manager: 'pip', value: '' }],
              }))
            }
          >
            <Plus className="size-4" aria-hidden />
            {msg('managedAgents.environments.packages.add', 'Add package')}
          </Button>
        </CardContent>
      </Card>
    </DetailCard>
  );
}

function EnvironmentMetadataEditor({
  values,
  error,
  rowErrors,
  onChange,
}: {
  values: EnvironmentEditValues;
  error?: string;
  rowErrors: Record<number, { key?: string; value?: string }>;
  onChange: (update: (current: EnvironmentEditValues) => EnvironmentEditValues) => void;
}) {
  const { msg } = useI18n();
  const updateRow = (index: number, patch: Partial<EnvironmentEditValues['metadataRows'][number]>) =>
    onChange((current) => ({
      ...current,
      metadataRows: current.metadataRows.map((row, rowIndex) => (rowIndex === index ? { ...row, ...patch } : row)),
    }));
  return (
    <DetailCard
      title={msg('managedAgents.environments.metadata.title', 'Metadata')}
      description={msg(
        'managedAgents.environments.metadata.description',
        'Add up to 16 unique key-value pairs. Keys can use uppercase or lowercase letters.',
      )}
    >
      <Card size="sm" className="py-0">
        <CardContent className="space-y-3 p-3">
          {values.metadataRows.map((row, index) => (
            <div key={index} className="grid gap-2 md:grid-cols-[1fr_1fr_40px]">
              <Field data-invalid={Boolean(rowErrors[index]?.key)}>
                <Input
                  aria-label={msg('managedAgents.environments.metadata.keyAria', 'Metadata key {number}', {
                    number: index + 1,
                  })}
                  value={row.key}
                  placeholder={msg('managedAgents.environments.metadata.keyPlaceholder', 'key')}
                  aria-invalid={Boolean(rowErrors[index]?.key) || undefined}
                  className="h-10"
                  onChange={(event) => updateRow(index, { key: event.target.value })}
                />
                {rowErrors[index]?.key ? <FieldError>{rowErrors[index].key}</FieldError> : null}
              </Field>
              <Field data-invalid={Boolean(rowErrors[index]?.value)}>
                <Input
                  aria-label={msg('managedAgents.environments.metadata.valueAria', 'Metadata value {number}', {
                    number: index + 1,
                  })}
                  value={row.value}
                  placeholder={msg('managedAgents.environments.metadata.valuePlaceholder', 'value')}
                  aria-invalid={Boolean(rowErrors[index]?.value) || undefined}
                  className="h-10"
                  onChange={(event) => updateRow(index, { value: event.target.value })}
                />
                {rowErrors[index]?.value ? <FieldError>{rowErrors[index].value}</FieldError> : null}
              </Field>
              <Button
                type="button"
                aria-label={msg('managedAgents.environments.metadata.remove', 'Remove metadata row {number}', {
                  number: index + 1,
                })}
                disabled={values.metadataRows.length <= 1}
                variant="secondary"
                size="icon-lg"
                className="size-10"
                onClick={() =>
                  onChange((current) => ({
                    ...current,
                    metadataRows: current.metadataRows.filter((_, rowIndex) => rowIndex !== index),
                  }))
                }
              >
                <X className="size-4" aria-hidden />
              </Button>
            </div>
          ))}
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <Button
            type="button"
            variant="secondary"
            size="lg"
            disabled={values.metadataRows.length >= 16}
            onClick={() =>
              onChange((current) => ({
                ...current,
                metadataRows: [...current.metadataRows, { key: '', value: '' }],
              }))
            }
          >
            <Plus className="size-4" aria-hidden />
            {msg('managedAgents.environments.metadata.add', 'Add metadata entry')}
          </Button>
        </CardContent>
      </Card>
    </DetailCard>
  );
}
