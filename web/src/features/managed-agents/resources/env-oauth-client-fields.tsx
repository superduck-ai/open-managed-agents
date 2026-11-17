import { type Ref, useEffect, useId, useImperativeHandle, useRef, useState } from 'react';
import type { CredentialFormValues } from '../types';
import { useI18n } from '../../../shared/i18n';
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { OptionalCredentialFields } from './optional-credential-fields';

export type EnvOAuthClientFieldsHandle = { validate: () => boolean };

export function EnvOAuthClientFields({
  values,
  onChange,
  clientRef,
  manualToken,
  saved,
}: {
  values: CredentialFormValues;
  onChange: (patch: Partial<CredentialFormValues>) => void;
  clientRef?: Ref<EnvOAuthClientFieldsHandle>;
  manualToken: boolean;
  saved: boolean;
}) {
  const { msg } = useI18n();
  const id = useId();
  const clientId = values.oauthClientId;
  const clientSecret = values.oauthClientSecret;
  const [open, setOpen] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const fieldsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (manualToken) setAttempted(false);
  }, [manualToken]);
  useEffect(() => {
    if (attempted && open) fieldsRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus();
  }, [attempted, open]);
  const missingId = !clientId.trim();
  const missingSecret =
    !clientSecret.trim() && (!saved || clientId.trim() !== values.envOAuthCustomProvider?.client_id);
  useImperativeHandle(clientRef, () => ({
    validate() {
      setAttempted(true);
      if (missingId || missingSecret) {
        setOpen(true);
        fieldsRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus();
        return false;
      }
      return true;
    },
  }));
  const fields = [
    {
      id: `${id}-client-id`,
      label: msg('platformAdmin.oauthManagement.clientId', 'Client ID'),
      type: 'text',
      value: clientId,
      setValue: (value: string) => onChange({ oauthClientId: value }),
      invalid: attempted && missingId,
      placeholder: undefined,
      error: msg(
        'managedAgents.credentialVaults.credentialDialog.clientIdRequired',
        'Custom OAuth authorization requires a client ID.',
      ),
    },
    {
      id: `${id}-client-secret`,
      label: msg('platformAdmin.oauthManagement.clientSecret', 'Client secret'),
      type: 'password',
      value: clientSecret,
      setValue: (value: string) => onChange({ oauthClientSecret: value }),
      invalid: attempted && missingSecret,
      placeholder:
        saved && clientId.trim() === values.envOAuthCustomProvider?.client_id
          ? msg('platformAdmin.oauthManagement.secretKeepHint', 'Leave blank to keep the current secret.')
          : undefined,
      error: msg(
        'managedAgents.credentialVaults.credentialDialog.clientSecretRequired',
        'Custom OAuth authorization requires a client secret.',
      ),
    },
  ];
  return (
    <OptionalCredentialFields
      title={msg('managedAgents.credentialVaults.credentialDialog.oauthClientConfiguration', 'Client configuration')}
      required={!manualToken || attempted}
      open={open}
      onOpenChange={setOpen}
    >
      <div ref={fieldsRef} className="grid gap-4 sm:grid-cols-2">
        {fields.map((field) => (
          <Field key={field.id}>
            <FieldLabel htmlFor={field.id}>{field.label}</FieldLabel>
            <Input
              id={field.id}
              type={field.type}
              value={field.value}
              onChange={(event) => field.setValue(event.target.value)}
              placeholder={field.placeholder}
              aria-invalid={field.invalid || undefined}
              aria-describedby={field.invalid ? `${field.id}-error` : undefined}
            />
            {field.invalid ? (
              <FieldDescription id={`${field.id}-error`} role="alert" className="text-destructive">
                {field.error}
              </FieldDescription>
            ) : null}
          </Field>
        ))}
      </div>
    </OptionalCredentialFields>
  );
}
