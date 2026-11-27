import { useState, type FormEvent } from 'react';
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
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Switch } from '../../../shared/ui/switch';
import {
  createRuntimeProfile,
  updateRuntimeProfile,
  type RuntimeProfileRecord,
} from '../../../shared/api/platform-admin';
import type { ApiError } from '../../../shared/api/client';
import { formatRuntimeProviderKind } from '../../../shared/workspaces/runtime';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';
import {
  buildRuntimeProfileCreateInput,
  buildRuntimeProfileUpdateInput,
  initialRuntimeProfileFormValues,
  QODER_CLOUD_DEFAULT_ENDPOINT,
  runtimeProfileFormReady,
  type RuntimeProfileFormValues,
} from './runtimeProfileForm';

const providerOptions = [{ value: 'qoder_cloud', label: 'Qoder Cloud' }];

export function RuntimeProfileDialog({
  profile,
  onClose,
  onSaved,
  onConflict,
}: {
  profile?: RuntimeProfileRecord;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onConflict: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const errorMessage = usePlatformAdminError();
  const [values, setValues] = useState<RuntimeProfileFormValues>(() => initialRuntimeProfileFormValues(profile));
  const [error, setError] = useState<string | null>(null);
  const patch = (next: Partial<RuntimeProfileFormValues>) => setValues((current) => ({ ...current, ...next }));

  const mutation = useMutation({
    mutationFn: () =>
      profile
        ? updateRuntimeProfile(client, profile.id, buildRuntimeProfileUpdateInput(values, profile))
        : createRuntimeProfile(client, buildRuntimeProfileCreateInput(values)),
    onSuccess: () => {
      void onSaved();
    },
    onError: async (submitError: unknown) => {
      setError(errorMessage(submitError));
      if ((submitError as ApiError).code === 'runtime_profile_version_conflict') {
        await onConflict();
      }
    },
  });

  const canSubmit = runtimeProfileFormReady(values, profile);
  const isBuiltin = Boolean(profile?.builtin);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || mutation.isPending) {
      return;
    }
    setError(null);
    mutation.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !mutation.isPending && onClose()}>
      <DialogContent className="sm:max-w-lg" closeButtonDisabled={mutation.isPending}>
        <DialogHeader>
          <DialogTitle>
            {profile
              ? msg('platformAdmin.runtimeProfiles.editTitle', 'Edit runtime profile')
              : msg('platformAdmin.runtimeProfiles.createTitle', 'Add runtime profile')}
          </DialogTitle>
          <DialogDescription>
            {msg(
              'platformAdmin.runtimeProfiles.dialogDescription',
              'Configure a shared platform runtime profile. Credentials are encrypted and never returned by the API.',
            )}
          </DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={handleSubmit}>
          <Field className="gap-2">
            <FieldLabel htmlFor="runtime-profile-provider">
              {msg('platformAdmin.runtimeProfiles.provider', 'Provider')}
            </FieldLabel>
            {profile ? (
              <Input
                id="runtime-profile-provider"
                value={formatRuntimeProviderKind(profile.provider_kind)}
                readOnly
                aria-readonly="true"
              />
            ) : (
              <Select
                value={values.providerKind}
                items={providerOptions}
                onValueChange={(next) => {
                  if (!next) return;
                  patch({
                    providerKind: next,
                    endpointUrl: next === 'qoder_cloud' ? QODER_CLOUD_DEFAULT_ENDPOINT : values.endpointUrl,
                  });
                }}
              >
                <SelectTrigger id="runtime-profile-provider" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {providerOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>

          <Field className="gap-2">
            <FieldLabel htmlFor="runtime-profile-name">{msg('platformAdmin.runtimeProfiles.name', 'Name')}</FieldLabel>
            <Input
              id="runtime-profile-name"
              value={values.name}
              placeholder={msg('platformAdmin.runtimeProfiles.namePlaceholder', 'e.g. Qoder Cloud CN')}
              disabled={mutation.isPending}
              onChange={(event) => patch({ name: event.target.value })}
            />
          </Field>

          {!isBuiltin ? (
            <>
              <Field className="gap-2">
                <FieldLabel htmlFor="runtime-profile-endpoint">
                  {msg('platformAdmin.runtimeProfiles.endpointUrl', 'Endpoint URL')}
                </FieldLabel>
                <Input
                  id="runtime-profile-endpoint"
                  type="url"
                  value={values.endpointUrl}
                  placeholder={QODER_CLOUD_DEFAULT_ENDPOINT}
                  disabled={mutation.isPending}
                  onChange={(event) => patch({ endpointUrl: event.target.value })}
                />
              </Field>

              <Field className="gap-2">
                <FieldLabel htmlFor="runtime-profile-credential">
                  {msg('platformAdmin.runtimeProfiles.credentialToken', 'Access token (PAT / SAT)')}
                  {profile?.has_credentials ? (
                    <span className="font-normal text-muted-foreground">
                      {' '}
                      {msg('managedAgents.common.optionalParen', '(optional)')}
                    </span>
                  ) : null}
                </FieldLabel>
                <Input
                  id="runtime-profile-credential"
                  type="password"
                  autoComplete="new-password"
                  value={values.credentialToken}
                  placeholder={
                    profile?.has_credentials
                      ? msg('platformAdmin.runtimeProfiles.credentialReplacePlaceholder', 'Enter a new token to rotate')
                      : msg('platformAdmin.runtimeProfiles.credentialPlaceholder', 'Paste provider PAT or SAT')
                  }
                  disabled={mutation.isPending}
                  onChange={(event) => patch({ credentialToken: event.target.value })}
                />
                <FieldDescription>
                  {profile?.has_credentials
                    ? msg(
                        'platformAdmin.runtimeProfiles.credentialKeepHint',
                        'Current credential ({hint}) is stored encrypted. Leave blank to keep it unchanged.',
                        { hint: profile.credential_hint || 'configured' },
                      )
                    : msg(
                        'platformAdmin.runtimeProfiles.credentialWriteOnlyHint',
                        'Write-only credential. Once saved, the raw token is never shown again.',
                      )}
                </FieldDescription>
              </Field>

              <div className="flex items-center justify-between gap-4 rounded-md border-[0.5px] border-border px-3 py-2.5">
                <div className="space-y-0.5">
                  <FieldLabel htmlFor="runtime-profile-status">
                    {msg('platformAdmin.runtimeProfiles.activeStatus', 'Active')}
                  </FieldLabel>
                  <FieldDescription>
                    {msg(
                      'platformAdmin.runtimeProfiles.activeStatusHint',
                      'Disable to prevent new workspaces from selecting this profile.',
                    )}
                  </FieldDescription>
                </div>
                <Switch
                  id="runtime-profile-status"
                  aria-label={msg('platformAdmin.runtimeProfiles.activeStatus', 'Active')}
                  checked={values.enabled}
                  disabled={mutation.isPending}
                  onCheckedChange={(checked) => patch({ enabled: checked })}
                />
              </div>
            </>
          ) : null}

          {error ? (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          ) : null}

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
