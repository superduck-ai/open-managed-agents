import { AlertTriangle } from 'lucide-react';

import { useI18n } from '../../../shared/i18n';
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Label } from '../../../shared/ui/label';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Textarea } from '../../../shared/ui/textarea';
import { cn } from '../../../shared/lib/utils';

type CredentialNetworkingFieldsProps = {
  networkType: 'limited' | 'unrestricted';
  allowedHostsText: string;
  hostsMissing: boolean;
  onChange: (patch: { networkType?: 'limited' | 'unrestricted'; allowedHostsText?: string }) => void;
};

export function CredentialNetworkingFields({
  networkType,
  allowedHostsText,
  hostsMissing,
  onChange,
}: CredentialNetworkingFieldsProps) {
  const { msg } = useI18n();
  const limited = msg('managedAgents.credentialVaults.credentialDialog.networkingLimited', 'Limited');
  const unrestricted = msg('managedAgents.credentialVaults.credentialDialog.networkingUnrestricted', 'Unrestricted');
  return (
    <div className="space-y-4">
      <Field className="gap-2">
        <FieldLabel>{msg('managedAgents.credentialVaults.credentialDialog.networking', 'Networking')}</FieldLabel>
        <RadioGroup
          value={networkType}
          aria-label={msg('managedAgents.credentialVaults.credentialDialog.networking', 'Networking')}
          className="grid grid-cols-2 gap-2"
          onValueChange={(value) => {
            if (value === 'limited' || value === 'unrestricted') {
              onChange({ networkType: value });
            }
          }}
        >
          <NetworkingOption value="limited" label={limited} selected={networkType === 'limited'} />
          <NetworkingOption value="unrestricted" label={unrestricted} selected={networkType === 'unrestricted'} />
        </RadioGroup>
      </Field>
      {networkType === 'limited' ? (
        <Field className="gap-2" data-invalid={hostsMissing || undefined}>
          <FieldLabel>
            {msg('managedAgents.credentialVaults.credentialDialog.allowedHosts', 'Allowed hosts')}
          </FieldLabel>
          <Textarea
            value={allowedHostsText}
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
        'flex h-9 cursor-pointer items-center justify-center gap-2 rounded-md border-[0.5px] border-input bg-background px-3 text-sm font-normal text-foreground transition-colors duration-150 ease-out motion-reduce:transition-none has-[:focus-visible]:ring-[0.5px] has-[:focus-visible]:ring-inset has-[:focus-visible]:ring-foreground',
        selected && 'border-foreground bg-accent text-foreground',
      )}
    >
      <RadioGroupItem id={id} value={value} className="sr-only" />
      {label}
    </Label>
  );
}
