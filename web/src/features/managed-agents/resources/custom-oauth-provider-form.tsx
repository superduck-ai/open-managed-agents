import { useState } from 'react';
import { ChevronLeft } from 'lucide-react';
import type { CustomOAuthProvider } from '../types';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';

export function CustomOAuthProviderForm({
  provider,
  onClose,
  onSaved,
}: {
  provider?: CustomOAuthProvider;
  onClose: () => void;
  onSaved: (provider: CustomOAuthProvider) => void;
}) {
  const { msg } = useI18n();
  const [values, setValues] = useState({
    name: provider?.name ?? '',
    authorizeEndpoint: provider?.authorize_endpoint ?? '',
    tokenEndpoint: provider?.token_endpoint ?? '',
    scope: provider?.scope ?? '',
  });
  const patch = (next: Partial<typeof values>) => setValues((current) => ({ ...current, ...next }));
  const ready = [values.name, values.authorizeEndpoint, values.tokenEndpoint].every((value) => value.trim());
  const fields = [
    {
      key: 'name',
      label: msg('platformAdmin.oauthManagement.name', 'Name'),
      placeholder: msg('managedAgents.common.namePlaceholder', 'Enter a name'),
    },
    {
      key: 'authorizeEndpoint',
      label: msg('platformAdmin.oauthManagement.authorizeEndpoint', 'Authorize endpoint'),
      placeholder: 'https://example.com/oauth/authorize',
    },
    {
      key: 'tokenEndpoint',
      label: msg('platformAdmin.oauthManagement.tokenEndpoint', 'Token endpoint'),
      placeholder: 'https://example.com/oauth/token',
    },
    {
      key: 'scope',
      label: msg('platformAdmin.oauthManagement.scope', 'Scope'),
      placeholder: 'read write offline_access',
    },
  ] as const;
  const title = provider
    ? msg('managedAgents.credentialVaults.credentialDialog.editProvider', 'Edit OAuth provider')
    : msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider');
  return (
    <form
      aria-label={title}
      className="space-y-4 p-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!ready) return;
        onSaved({
          name: values.name.trim(),
          authorize_endpoint: values.authorizeEndpoint.trim(),
          token_endpoint: values.tokenEndpoint.trim(),
          scope: values.scope.trim(),
          client_id: provider?.client_id,
        });
      }}
    >
      <Button
        type="button"
        variant="ghost"
        className="w-full justify-start border-b rounded-none pb-4"
        onClick={onClose}
      >
        <ChevronLeft className="size-4" aria-hidden />
        {title}
      </Button>
      <div className="grid gap-4 sm:grid-cols-2">
        {fields.map((field) => (
          <Field key={field.key}>
            <FieldLabel htmlFor={`custom-oauth-${field.key}`}>
              {field.label}
              {field.key === 'scope' ? (
                <span className="font-normal text-muted-foreground">
                  {msg('managedAgents.common.optionalParen', '(optional)')}
                </span>
              ) : null}
            </FieldLabel>
            <Input
              id={`custom-oauth-${field.key}`}
              autoFocus={field.key === 'name'}
              placeholder={field.placeholder}
              type={field.key === 'authorizeEndpoint' || field.key === 'tokenEndpoint' ? 'url' : 'text'}
              pattern={
                field.key === 'authorizeEndpoint' || field.key === 'tokenEndpoint'
                  ? '[Hh][Tt][Tt][Pp][Ss]://.*'
                  : undefined
              }
              title={
                field.key === 'authorizeEndpoint' || field.key === 'tokenEndpoint'
                  ? msg('managedAgents.credentialVaults.credentialDialog.httpsEndpointRequired', 'Use an HTTPS URL.')
                  : undefined
              }
              required={field.key !== 'scope'}
              value={values[field.key]}
              onChange={(event) => patch({ [field.key]: event.target.value })}
            />
          </Field>
        ))}
      </div>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onClose}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button type="submit" disabled={!ready}>
          {provider ? msg('common.save', 'Save') : msg('common.add', 'Add')}
        </Button>
      </div>
    </form>
  );
}
