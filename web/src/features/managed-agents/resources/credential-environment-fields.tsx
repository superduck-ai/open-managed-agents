import { AlertTriangle } from 'lucide-react';

import { useI18n } from '../../../shared/i18n';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Label } from '../../../shared/ui/label';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Textarea } from '../../../shared/ui/textarea';
import { cn } from '../../../shared/lib/utils';
import { ManagedTextField } from '../components/common';
import { type CredentialFormValues } from '../types';
import { credentialEnvHostsMissing } from './model';

type EnvironmentVariableCredentialFieldsProps = {
  values: CredentialFormValues;
  secretNameLocked: boolean;
  onChange: (patch: Partial<CredentialFormValues>) => void;
};

/**
 * CMA-aligned environment variable credential fields: variable pair, Credential
 * Networking, Injection Location. Submit gating lives in credentialFormReady.
 */
export function EnvironmentVariableCredentialFields({
  values,
  secretNameLocked,
  onChange,
}: EnvironmentVariableCredentialFieldsProps) {
  const { msg } = useI18n();
  const hostsMissing = credentialEnvHostsMissing(values);
  const limited = msg('managedAgents.credentialVaults.credentialDialog.networkingLimited', 'Limited');
  const unrestricted = msg('managedAgents.credentialVaults.credentialDialog.networkingUnrestricted', 'Unrestricted');

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <ManagedTextField
          label={msg('managedAgents.credentialVaults.credentialDialog.variableName', 'Variable name')}
          value={values.secretName}
          placeholder={msg('managedAgents.credentialVaults.credentialDialog.secretNamePlaceholder', 'MY_API_KEY')}
          disabled={secretNameLocked}
          onChange={(secretName) => onChange({ secretName })}
        />
        <ManagedTextField
          label={msg('managedAgents.credentialVaults.credentialDialog.value', 'Value')}
          type="password"
          value={values.secretValue}
          placeholder={msg('managedAgents.credentialVaults.credentialDialog.secretValue', 'Secret value')}
          onChange={(secretValue) => onChange({ secretValue })}
        />
      </div>

      <Field className="gap-2">
        <FieldLabel>{msg('managedAgents.credentialVaults.credentialDialog.networking', 'Networking')}</FieldLabel>
        <RadioGroup
          value={values.networkType}
          aria-label={msg('managedAgents.credentialVaults.credentialDialog.networking', 'Networking')}
          className="grid grid-cols-2 gap-2"
          onValueChange={(networkType) => {
            if (networkType === 'limited' || networkType === 'unrestricted') {
              onChange({ networkType });
            }
          }}
        >
          <NetworkingOption value="limited" label={limited} selected={values.networkType === 'limited'} />
          <NetworkingOption
            value="unrestricted"
            label={unrestricted}
            selected={values.networkType === 'unrestricted'}
          />
        </RadioGroup>
      </Field>

      {values.networkType === 'limited' ? (
        <Field className="gap-2" data-invalid={hostsMissing || undefined}>
          <FieldLabel>
            {msg('managedAgents.credentialVaults.credentialDialog.allowedHosts', 'Allowed hosts')}
          </FieldLabel>
          <Textarea
            value={values.allowedHostsText}
            aria-invalid={hostsMissing || undefined}
            aria-label={msg('managedAgents.credentialVaults.credentialDialog.allowedHosts', 'Allowed hosts')}
            placeholder={msg(
              'managedAgents.credentialVaults.credentialDialog.allowedHostsPlaceholder',
              'api.example.com, *.example.com',
            )}
            rows={3}
            className={cn('font-mono text-sm', hostsMissing && 'border-destructive')}
            onChange={(event) => onChange({ allowedHostsText: event.target.value })}
          />
          {hostsMissing ? (
            <p className="flex items-center gap-1.5 text-sm text-destructive" role="alert">
              <AlertTriangle className="size-3.5 shrink-0" aria-hidden />
              {msg(
                'managedAgents.credentialVaults.credentialDialog.allowedHostsRequired',
                'At least one host is required for limited networking.',
              )}
            </p>
          ) : null}
          <FieldDescription>
            {msg(
              'managedAgents.credentialVaults.credentialDialog.allowedHostsHint',
              'Separate hosts with commas or newlines.',
            )}
          </FieldDescription>
        </Field>
      ) : null}

      <Field className="gap-2">
        <FieldLabel>
          {msg('managedAgents.credentialVaults.credentialDialog.injectionLocation', 'Injection location')}
        </FieldLabel>
        <div className="space-y-2">
          <InjectionLocationRow
            id="credential-inject-header"
            checked={values.injectHeader}
            label={msg('managedAgents.credentialVaults.credentialDialog.injectionHeader', 'Request headers')}
            onCheckedChange={(injectHeader) => onChange({ injectHeader })}
          />
          <InjectionLocationRow
            id="credential-inject-body"
            checked={values.injectBody}
            label={msg('managedAgents.credentialVaults.credentialDialog.injectionBody', 'Request body')}
            onCheckedChange={(injectBody) => onChange({ injectBody })}
          />
        </div>
        <FieldDescription>
          {msg(
            'managedAgents.credentialVaults.credentialDialog.injectionHint',
            'Limiting to request headers is recommended unless the service reads the secret from the request body.',
          )}
        </FieldDescription>
      </Field>
    </div>
  );
}

function NetworkingOption({
  value,
  label,
  selected,
}: {
  value: 'limited' | 'unrestricted';
  label: string;
  selected: boolean;
}) {
  const id = `credential-networking-${value}`;
  return (
    <Label
      htmlFor={id}
      className={cn(
        'flex h-9 cursor-pointer items-center justify-center gap-2 rounded-md border border-input bg-background px-3 text-sm font-normal text-foreground transition-colors',
        selected && 'border-primary bg-primary/5 text-primary',
      )}
    >
      <RadioGroupItem id={id} value={value} className="sr-only" aria-label={label} />
      {label}
    </Label>
  );
}

function InjectionLocationRow({
  id,
  checked,
  label,
  onCheckedChange,
}: {
  id: string;
  checked: boolean;
  label: string;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Checkbox id={id} checked={checked} onCheckedChange={(value) => onCheckedChange(value === true)} />
      <Label htmlFor={id} className="text-sm font-normal text-foreground">
        {label}
      </Label>
    </div>
  );
}
