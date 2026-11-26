import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import {
  getOrganizationRuntimePolicy,
  updateOrganizationRuntimePolicy,
  type OrganizationRuntimePolicyRecord,
  type RuntimeProfileRecord,
} from '../../../shared/api/platform-admin';
import { DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_UUID, formatRuntimeProviderKind } from '../../../shared/workspaces/runtime';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';

function isBuiltinProfile(profile: RuntimeProfileRecord) {
  return profile.builtin || profile.uuid === DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_UUID;
}

export function OrganizationRuntimePolicySection({ organizationUuid }: { organizationUuid: string }) {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const queryClient = useQueryClient();
  const errorMessage = usePlatformAdminError();
  const queryKey = ['platform-admin', 'organization-runtime-policy', organizationUuid] as const;

  const query = useQuery({
    queryKey,
    queryFn: () => getOrganizationRuntimePolicy(client, organizationUuid),
  });

  const [allowedUuids, setAllowedUuids] = useState<string[]>([]);
  const [defaultUuid, setDefaultUuid] = useState('');
  const [notice, setNotice] = useState('');
  const [submitError, setSubmitError] = useState('');

  useEffect(() => {
    if (!query.data) return;
    setAllowedUuids(query.data.allowed_runtime_profile_uuids);
    setDefaultUuid(query.data.default_runtime_profile_uuid);
  }, [query.data]);

  const saveMutation = useMutation({
    mutationFn: () =>
      updateOrganizationRuntimePolicy(client, organizationUuid, {
        default_runtime_profile_uuid: defaultUuid,
        allowed_runtime_profile_uuids: allowedUuids,
      }),
    onSuccess: (updated: OrganizationRuntimePolicyRecord) => {
      queryClient.setQueryData(queryKey, updated);
      setAllowedUuids(updated.allowed_runtime_profile_uuids);
      setDefaultUuid(updated.default_runtime_profile_uuid);
      setSubmitError('');
      setNotice(msg('platformAdmin.runtimePolicySection.saved', 'Organization runtime policy saved.'));
    },
    onError: (error: unknown) => {
      setNotice('');
      setSubmitError(errorMessage(error));
    },
  });

  if (query.isPending) {
    return <p role="status">{msg('common.loading', 'Loading...')}</p>;
  }

  if (query.isError) {
    return (
      <div role="alert" className="space-y-3 text-sm text-destructive">
        <p>{errorMessage(query.error)}</p>
        <Button type="button" variant="outline" size="sm" onClick={() => void query.refetch()}>
          {msg('platformAdmin.retry', 'Retry')}
        </Button>
      </div>
    );
  }

  const availableProfiles = query.data.profiles.filter((profile) => profile.status !== 'archived');
  const activeAllowedProfiles = availableProfiles.filter(
    (profile) => profile.status === 'active' && allowedUuids.includes(profile.uuid),
  );
  const defaultSelectItems = activeAllowedProfiles.map((profile) => ({
    value: profile.uuid,
    label: profile.name,
  }));

  const toggleProfile = (profile: RuntimeProfileRecord, checked: boolean) => {
    if (isBuiltinProfile(profile)) {
      return;
    }
    setNotice('');
    setSubmitError('');
    setAllowedUuids((current) => {
      const next = checked
        ? Array.from(new Set([...current, profile.uuid]))
        : current.filter((uuid) => uuid !== profile.uuid);
      if (!checked && defaultUuid === profile.uuid) {
        const fallback = availableProfiles.find((candidate) => isBuiltinProfile(candidate))?.uuid ?? next[0] ?? '';
        setDefaultUuid(fallback);
      }
      return next;
    });
  };

  return (
    <section className="max-w-2xl space-y-6">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">
          {msg('platformAdmin.runtimePolicySection.title', 'Runtime authorization')}
        </h2>
        <p className="text-sm text-muted-foreground">
          {msg(
            'platformAdmin.runtimePolicySection.description',
            'Choose which shared runtime profiles this organization can select when creating new workspaces.',
          )}
        </p>
      </div>

      <div className="space-y-2">
        <p className="text-sm font-medium">
          {msg('platformAdmin.runtimePolicySection.allowedProfiles', 'Allowed runtime profiles')}
        </p>
        <div className="divide-y-[0.5px] divide-border rounded-lg border-[0.5px] border-border">
          {availableProfiles.map((profile) => {
            const builtin = isBuiltinProfile(profile);
            const checked = builtin || allowedUuids.includes(profile.uuid);
            const checkboxId = `org-runtime-profile-${profile.uuid}`;
            return (
              <div key={profile.uuid} className="flex items-start gap-3 px-4 py-3">
                <Checkbox
                  id={checkboxId}
                  checked={checked}
                  disabled={builtin || saveMutation.isPending}
                  onCheckedChange={(nextChecked) => toggleProfile(profile, Boolean(nextChecked))}
                  className="mt-0.5"
                />
                <label htmlFor={checkboxId} className="min-w-0 flex-1 cursor-pointer space-y-0.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium text-foreground">{profile.name}</span>
                    <Badge variant="outline">{formatRuntimeProviderKind(profile.provider_kind)}</Badge>
                    {builtin ? (
                      <Badge variant="secondary">{msg('platformAdmin.runtimeProfiles.builtin', 'Built-in')}</Badge>
                    ) : null}
                    {profile.status !== 'active' ? (
                      <Badge variant="outline">{msg('platformAdmin.runtimeProfiles.disabled', 'Disabled')}</Badge>
                    ) : null}
                  </div>
                  <p className="font-mono text-xs text-muted-foreground">{profile.id}</p>
                </label>
              </div>
            );
          })}
        </div>
      </div>

      <Field className="gap-2">
        <FieldLabel htmlFor="organization-default-runtime-profile">
          {msg('platformAdmin.runtimePolicySection.defaultProfile', 'Default runtime profile')}
        </FieldLabel>
        <Select
          value={defaultUuid}
          items={defaultSelectItems}
          disabled={saveMutation.isPending || activeAllowedProfiles.length === 0}
          onValueChange={(next) => {
            if (!next) return;
            setNotice('');
            setSubmitError('');
            setDefaultUuid(next);
          }}
        >
          <SelectTrigger id="organization-default-runtime-profile" className="w-full sm:max-w-md">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {activeAllowedProfiles.map((profile) => (
              <SelectItem key={profile.uuid} value={profile.uuid} label={profile.name}>
                {profile.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FieldDescription>
          {msg(
            'platformAdmin.runtimePolicySection.defaultProfileHint',
            'Used as the pre-selected runtime when organization admins create a new workspace.',
          )}
        </FieldDescription>
      </Field>

      {notice ? (
        <p role="status" className="text-sm text-foreground">
          {notice}
        </p>
      ) : null}
      {submitError ? (
        <p role="alert" className="text-sm text-destructive">
          {submitError}
        </p>
      ) : null}

      <div>
        <Button
          type="button"
          disabled={saveMutation.isPending || !defaultUuid || allowedUuids.length === 0}
          onClick={() => saveMutation.mutate()}
        >
          {saveMutation.isPending ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
        </Button>
      </div>
    </section>
  );
}
