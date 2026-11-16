import { useState, type ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { useMutation } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { Textarea } from '../../../shared/ui/textarea';
import { Input } from '../../../shared/ui/input';
import { Field as FormField, FieldLabel, FieldDescription } from '../../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { createOAuthProvider, updateOAuthProvider, type OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';
import type { ApiError } from '../../../shared/api/client';
import {
  initialOAuthProviderFormValues,
  oauthProviderFormReady,
  oauthProviderOptionalFields,
  type OAuthProviderFormValues,
} from './oauthProviderForm';

export function OAuthProviderDialog({
  provider,
  onClose,
  onSaved,
  onConflict,
}: {
  provider?: OAuthProviderRecord;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onConflict: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const errorMessage = usePlatformAdminError();
  const [values, setValues] = useState<OAuthProviderFormValues>(() => initialOAuthProviderFormValues(provider));
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const patch = (next: Partial<OAuthProviderFormValues>) => setValues((current) => ({ ...current, ...next }));
  const mutation = useMutation({
    mutationFn: () => {
      const editable = {
        scope: values.scope.trim(),
        default_secret_name: values.defaultSecretName.trim(),
        default_allowed_hosts: values.defaultAllowedHostsText
          .split(/[\n,]/)
          .map((host) => host.trim())
          .filter(Boolean),
        logo_url: values.logoUrl.trim(),
        ...(values.clientId.trim() ? { client_id: values.clientId.trim() } : {}),
        ...(values.clientSecret.trim() ? { client_secret: values.clientSecret.trim() } : {}),
      };
      const input = {
        ...editable,
        name: values.name.trim(),
        authorize_endpoint: values.authorizeEndpoint.trim(),
        token_endpoint: values.tokenEndpoint.trim(),
        token_endpoint_auth_method: values.authMethod,
        token_endpoint_encoding: values.encoding,
      };
      return provider
        ? updateOAuthProvider(client, provider.id, { ...input, expected_version: provider.version })
        : createOAuthProvider(client, { ...input, client_id: values.clientId.trim() });
    },
    onSuccess: () => {
      void onSaved();
    },
    onError: async (submitError: unknown) => {
      setError(errorMessage(submitError));
      if ((submitError as ApiError).code === 'oauth_provider_version_conflict') await onConflict();
    },
  });
  const canSubmit = oauthProviderFormReady(values, provider);
  const optionalFields = oauthProviderOptionalFields(values, provider);
  return (
    <Dialog open onOpenChange={(open) => !open && !mutation.isPending && onClose()}>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-2xl"
        closeButtonDisabled={mutation.isPending}
      >
        <DialogHeader>
          <DialogTitle>
            {provider
              ? msg('platformAdmin.oauthManagement.editTitle', 'Edit OAuth client')
              : msg('platformAdmin.oauthManagement.createTitle', 'Register OAuth client')}
          </DialogTitle>
          <DialogDescription>
            {msg(
              'platformAdmin.oauthManagement.dialogDescription',
              'Client secret is encrypted and never returned by the API.',
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto] gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (canSubmit && !mutation.isPending) {
              setError(null);
              mutation.mutate();
            }
          }}
        >
          <div className="min-h-0 space-y-4 overflow-y-auto [overflow-wrap:anywhere]">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="oauth-provider-name"
                label={msg('platformAdmin.oauthManagement.name', 'Name')}
                className="sm:col-span-2"
              >
                <Input
                  id="oauth-provider-name"
                  value={values.name}
                  onChange={(event) => patch({ name: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-authorize"
                label={msg('platformAdmin.oauthManagement.authorizeEndpoint', 'Authorize endpoint')}
                className="sm:col-span-2"
              >
                <Input
                  id="oauth-provider-authorize"
                  value={values.authorizeEndpoint}
                  placeholder="https://example.com/oauth/authorize"
                  onChange={(event) => patch({ authorizeEndpoint: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-token"
                label={msg('platformAdmin.oauthManagement.tokenEndpoint', 'Token endpoint')}
                className="sm:col-span-2"
              >
                <Input
                  id="oauth-provider-token"
                  value={values.tokenEndpoint}
                  placeholder="https://example.com/oauth/token"
                  onChange={(event) => patch({ tokenEndpoint: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-logo"
                optional
                label={msg('platformAdmin.oauthManagement.logoUrl', 'Logo URL')}
                className="sm:col-span-2"
              >
                <Input
                  id="oauth-provider-logo"
                  type="url"
                  value={values.logoUrl}
                  placeholder="https://example.com/logo.png"
                  onChange={(event) => patch({ logoUrl: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-default-secret-name"
                optional
                label={msg('platformAdmin.oauthManagement.defaultSecretName', 'Default variable name')}
                className="sm:col-span-2"
              >
                <Input
                  id="oauth-provider-default-secret-name"
                  value={values.defaultSecretName}
                  placeholder="ACCESS_TOKEN"
                  onChange={(event) => patch({ defaultSecretName: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-default-hosts"
                optional
                label={msg('platformAdmin.oauthManagement.defaultAllowedHosts', 'Default allowed hosts')}
                className="sm:col-span-2"
              >
                <Textarea
                  id="oauth-provider-default-hosts"
                  value={values.defaultAllowedHostsText}
                  placeholder="api.example.com"
                  onChange={(event) => patch({ defaultAllowedHostsText: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-client-id"
                label={msg('platformAdmin.oauthManagement.clientId', 'Client ID')}
                optional={optionalFields.clientId}
              >
                <Input
                  id="oauth-provider-client-id"
                  value={values.clientId}
                  onChange={(event) => patch({ clientId: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-secret"
                optional={optionalFields.clientSecret}
                label={msg('platformAdmin.oauthManagement.clientSecret', 'Client secret')}
                hint={
                  provider?.configured
                    ? msg('platformAdmin.oauthManagement.secretKeepHint', 'Leave blank to keep the current secret.')
                    : undefined
                }
              >
                <Input
                  id="oauth-provider-secret"
                  type="password"
                  value={values.clientSecret}
                  onChange={(event) => patch({ clientSecret: event.target.value })}
                />
              </Field>
              <Field
                id="oauth-provider-scope"
                optional={optionalFields.scope}
                label={msg('platformAdmin.oauthManagement.scope', 'Scope')}
                className="sm:col-span-2"
              >
                <Input
                  id="oauth-provider-scope"
                  value={values.scope}
                  onChange={(event) => patch({ scope: event.target.value })}
                />
              </Field>
            </div>
            <Collapsible
              open={advancedOpen}
              onOpenChange={setAdvancedOpen}
              className="border-t-[0.5px] border-border pt-3"
            >
              <CollapsibleTrigger
                type="button"
                className="flex min-h-9 w-full items-center gap-2 text-left text-sm font-medium outline-none focus-visible:ring-[0.5px] focus-visible:ring-foreground"
              >
                <ChevronRight
                  className={`size-4 transition-transform ${advancedOpen ? 'rotate-90' : ''}`}
                  aria-hidden
                />
                {msg('platformAdmin.oauthManagement.advanced', 'Advanced configuration')}
              </CollapsibleTrigger>
              <CollapsibleContent className="grid gap-4 pt-3 sm:grid-cols-2">
                <Field
                  id="oauth-provider-auth-method"
                  label={msg('platformAdmin.oauthManagement.authMethod', 'Token endpoint auth')}
                >
                  <Select value={values.authMethod} onValueChange={(value) => value && patch({ authMethod: value })}>
                    <SelectTrigger id="oauth-provider-auth-method">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="client_secret_post">client_secret_post</SelectItem>
                      <SelectItem value="client_secret_basic">client_secret_basic</SelectItem>
                      <SelectItem value="none">none</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <Field
                  id="oauth-provider-encoding"
                  label={msg('platformAdmin.oauthManagement.encoding', 'Token encoding')}
                >
                  <Select value={values.encoding} onValueChange={(value) => value && patch({ encoding: value })}>
                    <SelectTrigger id="oauth-provider-encoding">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="form">form</SelectItem>
                      <SelectItem value="json">json</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              </CollapsibleContent>
            </Collapsible>
            {provider && values.clientId.trim() !== provider.client_id ? (
              <FieldDescription>
                {msg(
                  'platformAdmin.oauthManagement.changeAppHint',
                  'Changing the client ID requires a new secret. Existing tokens may need reauthorization.',
                )}
              </FieldDescription>
            ) : null}
            {provider && !provider.configured ? (
              <FieldDescription>
                {msg(
                  'platformAdmin.oauthManagement.firstConfiguration',
                  'Enter both client ID and client secret to configure this provider.',
                )}
              </FieldDescription>
            ) : null}
            {error ? (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={mutation.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={!canSubmit || mutation.isPending}>
              {mutation.isPending ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({
  id,
  label,
  optional = false,
  hint,
  className,
  children,
}: {
  id: string;
  label: string;
  optional?: boolean;
  hint?: string;
  className?: string;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  return (
    <FormField className={className}>
      <FieldLabel htmlFor={id}>
        {label}
        {optional ? (
          <span className="font-normal text-muted-foreground">
            {msg('managedAgents.common.optionalParen', '(optional)')}
          </span>
        ) : null}
      </FieldLabel>
      {children}
      {hint ? <FieldDescription>{hint}</FieldDescription> : null}
    </FormField>
  );
}
