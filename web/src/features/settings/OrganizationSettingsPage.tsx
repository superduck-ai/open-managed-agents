import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { Copy, Save, X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState, type InputHTMLAttributes } from 'react';
import { SettingsShell } from '../../app/layout/ConsoleLayout';
import { LimitsPage, ServiceAccountsPage } from '../dashboard/DashboardPage';
import { PrivacyControlsPage } from '../dashboard/privacy-controls';
import { useAuth } from '../../shared/auth/context';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Button } from '../../shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '../../shared/ui/card';
import { Field, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '../../shared/ui/select';
import { Skeleton } from '../../shared/ui/skeleton';
import { Switch } from '../../shared/ui/switch';
import {
  getOrganization,
  getOrganizationProfile,
  updateOrganization,
  updateOrganizationProfile,
  type Organization,
  type OrganizationPhysicalAddress,
  type OrganizationProfile,
  type UpdateOrganizationInput,
  type UpdateOrganizationProfileInput
} from '../../shared/organization/api';
import { useWorkspace } from '../../shared/workspaces/context';
import { BillingSettingsPage } from './BillingSettingsPage';
import { AdminKeysSettingsPage } from './AdminKeysSettingsPage';
import { IdentityAndAccessSettingsPage } from './IdentityAndAccessSettingsPage';
import { WorkloadIdentitySettingsPage } from './WorkloadIdentitySettingsPage';
import {
  AppearanceSettingsPage,
  ProfileSettingsPage,
  SettingsApiKeysPage,
  settingsSectionFromPath,
  type SettingsPageSection
} from './feature-pages';
import { OrganizationMembersPage } from './OrganizationMembersPage';
import { WorkspacesSettingsPage } from './WorkspacesSettingsPage';

const countryOptions = [
  { value: 'US', label: 'United States' },
  { value: 'CA', label: 'Canada' },
  { value: 'GB', label: 'United Kingdom' }
];

type FormState = {
  name: string;
  line1: string;
  line2: string;
  country: string;
  state: string;
  city: string;
  postalCode: string;
};

const emptyForm: FormState = {
  name: '',
  line1: '',
  line2: '',
  country: '',
  state: '',
  city: '',
  postalCode: ''
};

type OrganizationSettingsSection = Extract<
  SettingsPageSection,
  | 'organization'
  | 'limits'
  | 'members'
  | 'workspaces'
  | 'service-accounts'
  | 'profile'
  | 'appearance'
  | 'billing'
  | 'api-keys'
  | 'admin-keys'
  | 'workload-identity'
  | 'privacy-controls'
  | 'identity-and-access'
>;

export function OrganizationSettingsPage({ section = 'organization' }: { section?: OrganizationSettingsSection }) {
  const { account, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const resolvedSection = section === 'organization' ? settingsSectionFromPath(location.pathname) : section;

  const handleNavigate = async (href: string) => {
    await navigate({ href });
  };

  const handleLogout = async () => {
    await logout();
    await navigate({ to: '/login', search: { returnTo: '/' } });
  };

  return (
    <SettingsShell
      account={account}
      currentPath={location.pathname}
      onLogout={handleLogout}
      onNavigate={handleNavigate}
    >
      {resolvedSection === 'profile' ? <ProfileSettingsPage /> : null}
      {resolvedSection === 'appearance' ? <AppearanceSettingsPage /> : null}
      {resolvedSection === 'organization' ? <OrganizationSettingsContent /> : null}
      {resolvedSection === 'members' ? <OrganizationMembersPage /> : null}
      {resolvedSection === 'workspaces' ? <WorkspacesSettingsPage /> : null}
      {resolvedSection === 'billing' ? <BillingSettingsPage /> : null}
      {resolvedSection === 'limits' ? <LimitsPage /> : null}
      {resolvedSection === 'api-keys' ? <SettingsApiKeysPage /> : null}
      {resolvedSection === 'admin-keys' ? <AdminKeysSettingsPage /> : null}
      {resolvedSection === 'service-accounts' ? <ServiceAccountsPage /> : null}
      {resolvedSection === 'workload-identity' ? <WorkloadIdentitySettingsPage /> : null}
      {resolvedSection === 'privacy-controls' ? <PrivacyControlsPage /> : null}
      {resolvedSection === 'identity-and-access' ? <IdentityAndAccessSettingsPage /> : null}
    </SettingsShell>
  );
}

export function OrganizationSettingsContent() {
  const { account, csrfToken, refresh } = useAuth();
  const { orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const bootstrapOrganization = account?.memberships?.find((membership) => membership.organization?.uuid)?.organization;
  const activeOrgUuid = orgUuid ?? bootstrapOrganization?.uuid;
  const initialForm = useMemo<FormState>(
    () => ({ ...emptyForm, name: bootstrapOrganization?.name ?? '' }),
    [bootstrapOrganization?.name]
  );

  const organizationQuery = useQuery({
    queryKey: ['console', 'organization', activeOrgUuid],
    queryFn: () => getOrganization(activeOrgUuid ?? ''),
    enabled: Boolean(activeOrgUuid),
    retry: false
  });
  const profileQuery = useQuery({
    queryKey: ['console', 'organization-profile', activeOrgUuid],
    queryFn: () => getOrganizationProfile(activeOrgUuid ?? ''),
    enabled: Boolean(activeOrgUuid),
    retry: false
  });

  const [form, setForm] = useState<FormState>(() => initialForm);
  const [savedForm, setSavedForm] = useState<FormState>(() => initialForm);
  const savedFormRef = useRef<FormState>(initialForm);
  const hydratedOrgRef = useRef<string | null>(null);
  const [allowApiKeys, setAllowApiKeys] = useState(true);
  const [savedAllowApiKeys, setSavedAllowApiKeys] = useState(true);
  const [copied, setCopied] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [switchError, setSwitchError] = useState<string | null>(null);

  useEffect(() => {
    savedFormRef.current = savedForm;
  }, [savedForm]);

  useEffect(() => {
    if (!organizationQuery.data || !profileQuery.data) {
      return;
    }
    const next = formFromOrganization(organizationQuery.data, profileQuery.data);
    const apiKeysEnabled = organizationAllowsApiKeys(organizationQuery.data);
    const shouldHydrateOrg = hydratedOrgRef.current !== (activeOrgUuid ?? null);
    setForm((current) =>
      shouldHydrateOrg || sameForm(normalizeForm(current), normalizeForm(savedFormRef.current)) ? next : current
    );
    hydratedOrgRef.current = activeOrgUuid ?? null;
    savedFormRef.current = next;
    setSavedForm(next);
    setAllowApiKeys(apiKeysEnabled);
    setSavedAllowApiKeys(apiKeysEnabled);
    setFormError(null);
    setSwitchError(null);
  }, [activeOrgUuid, organizationQuery.data, profileQuery.data]);

  const updateOrganizationMutation = useMutation({
    mutationFn: (input: UpdateOrganizationInput) => updateOrganization(activeOrgUuid ?? '', input, csrfToken),
    onSuccess: async (organization) => {
      queryClient.setQueryData(['console', 'organization', activeOrgUuid], organization);
      await queryClient.invalidateQueries({ queryKey: ['auth', 'bootstrap'] });
      await refresh();
    }
  });

  const updateProfileMutation = useMutation({
    mutationFn: (input: UpdateOrganizationProfileInput) => updateOrganizationProfile(activeOrgUuid ?? '', input, csrfToken),
    onSuccess: (profile) => {
      queryClient.setQueryData(['console', 'organization-profile', activeOrgUuid], profile);
    }
  });

  const normalizedForm = useMemo(() => normalizeForm(form), [form]);
  const normalizedSavedForm = useMemo(() => normalizeForm(savedForm), [savedForm]);
  const isDirty = !sameForm(normalizedForm, normalizedSavedForm);
  const addressValid = isAddressValid(normalizedForm);
  const isSaving = updateOrganizationMutation.isPending || updateProfileMutation.isPending;
  const canSave = Boolean(activeOrgUuid) && isDirty && addressValid && normalizedForm.name !== '' && !isSaving;
  const organizationId = organizationQuery.data?.uuid ?? activeOrgUuid ?? '';

  const copyOrganizationId = async () => {
    try {
      await navigator.clipboard?.writeText(organizationId);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  };

  const handleSave = async () => {
    if (!canSave) {
      return;
    }
    setFormError(null);
    try {
      if (normalizedForm.name !== normalizedSavedForm.name) {
        await updateOrganizationMutation.mutateAsync({ name: normalizedForm.name });
      }
      if (!sameAddressPayload(addressPayload(normalizedForm), addressPayload(normalizedSavedForm))) {
        await updateProfileMutation.mutateAsync({ physical_address: addressPayload(normalizedForm) });
      }
      setForm(normalizedForm);
      setSavedForm(normalizedForm);
    } catch (error) {
      setFormError(errorMessage(error));
    }
  };

  const handleCancel = () => {
    setForm(savedForm);
    setFormError(null);
  };

  const handleApiKeysToggle = async () => {
    if (!activeOrgUuid || updateOrganizationMutation.isPending) {
      return;
    }
    const next = !allowApiKeys;
    setAllowApiKeys(next);
    setSwitchError(null);
    try {
      await updateOrganizationMutation.mutateAsync({
        default_workspace_settings: {
          enable_api_keys: next
        }
      });
      setSavedAllowApiKeys(next);
    } catch (error) {
      setAllowApiKeys(savedAllowApiKeys);
      setSwitchError(errorMessage(error));
    }
  };

  if (!activeOrgUuid) {
    return (
      <section className="mx-auto w-full max-w-[1100px]">
        <Card>
          <CardHeader>
            <CardTitle>Organization</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">No organization is available for this session.</p>
          </CardContent>
        </Card>
      </section>
    );
  }

  const isInitialLoading =
    (organizationQuery.isLoading || profileQuery.isLoading) && (!organizationQuery.data || !profileQuery.data);

  return (
    <section className="mx-auto w-full max-w-[1100px] space-y-4" data-testid="organization-settings-page">
      <Card>
        <CardHeader>
          <h1 className="text-xl font-semibold tracking-normal text-foreground">Organization</h1>
        </CardHeader>
        <CardContent className="space-y-7">
          {isInitialLoading ? (
            <div className="space-y-4" aria-label="Loading organization settings">
              <Skeleton className="h-10 w-[428px] max-w-full" />
              <Skeleton className="h-10 w-[428px] max-w-full" />
              <Skeleton className="h-10 w-[720px] max-w-full" />
            </div>
          ) : (
            <>
              <Field className="max-w-[428px] gap-2">
                <FieldLabel htmlFor="organization-name">Organization name</FieldLabel>
                <TextInput
                  id="organization-name"
                  value={form.name}
                  onChange={(name) => setForm((current) => ({ ...current, name }))}
                />
              </Field>

              <div className="space-y-6">
                <div className="text-sm font-medium text-foreground">Primary business address</div>
                <div className="grid gap-3 lg:grid-cols-[208px_208px]">
                  <TextInput
                    aria-label="Primary business address line 1"
                    placeholder="Line 1"
                    value={form.line1}
                    onChange={(line1) => setForm((current) => ({ ...current, line1 }))}
                  />
                  <TextInput
                    aria-label="Primary business address line 2"
                    placeholder="Line 2"
                    value={form.line2}
                    onChange={(line2) => setForm((current) => ({ ...current, line2 }))}
                  />
                </div>

                <div className="grid gap-3 lg:grid-cols-[208px_208px_minmax(180px,1fr)_96px]">
                  <div>
                    <Label id="country-label" className="mb-2">
                      Country
                    </Label>
                    <CountryCombobox
                      value={form.country}
                      onChange={(country) => setForm((current) => ({ ...current, country }))}
                    />
                  </div>
                  <TextField
                    id="state"
                    label="State or province"
                    value={form.state}
                    onChange={(state) => setForm((current) => ({ ...current, state }))}
                  />
                  <TextField
                    id="city"
                    label="City"
                    value={form.city}
                    onChange={(city) => setForm((current) => ({ ...current, city }))}
                  />
                  <TextField
                    id="postal-code"
                    label="Postal code"
                    value={form.postalCode}
                    onChange={(postalCode) => setForm((current) => ({ ...current, postalCode }))}
                  />
                </div>

                {!addressValid ? (
                  <Alert variant="destructive">
                    <AlertDescription>
                      Enter line 1, country, state, city, and postal code, or leave the address blank.
                    </AlertDescription>
                  </Alert>
                ) : null}
              </div>

              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>Organization ID: {organizationId}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="text-muted-foreground hover:text-foreground"
                  aria-label="Copy Organization ID"
                  onClick={copyOrganizationId}
                >
                  <Copy className="size-3.5" aria-hidden />
                </Button>
                {copied ? <span className="text-primary">Copied</span> : null}
              </div>

              {isDirty ? (
                <div className="space-y-3">
                  <div className="flex flex-wrap items-center gap-3">
                    <Button
                      type="button"
                      size="lg"
                      disabled={!canSave}
                      onClick={handleSave}
                    >
                      <Save className="size-4" aria-hidden />
                      {isSaving ? 'Saving' : 'Save changes'}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="lg"
                      onClick={handleCancel}
                    >
                      <X className="size-4" aria-hidden />
                      Cancel
                    </Button>
                  </div>
                  {formError ? (
                    <Alert variant="destructive">
                      <AlertDescription>{formError}</AlertDescription>
                    </Alert>
                  ) : null}
                </div>
              ) : formError ? (
                <Alert variant="destructive">
                  <AlertDescription>{formError}</AlertDescription>
                </Alert>
              ) : null}
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="flex items-start justify-between gap-8">
          <div className="max-w-[760px]">
            <h2 className="text-xl font-semibold tracking-normal text-foreground">
              Allow creating new API keys in default workspace
            </h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              Allow users to create new API keys in the default workspace. Disabling this setting does not affect existing
              API keys or disable Workbench usage.
            </p>
            {switchError ? (
              <Alert variant="destructive" className="mt-3">
                <AlertDescription>{switchError}</AlertDescription>
              </Alert>
            ) : null}
          </div>
          <Switch
            checked={allowApiKeys}
            aria-label="Allow creating new API keys in default workspace"
            className="mt-1"
            disabled={updateOrganizationMutation.isPending}
            onCheckedChange={() => void handleApiKeysToggle()}
          />
        </CardContent>
      </Card>
    </section>
  );
}

function TextField({
  id,
  label,
  value,
  onChange
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Field className="gap-2">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <TextInput id={id} value={value} onChange={onChange} />
    </Field>
  );
}

function TextInput({
  value,
  onChange,
  ...props
}: {
  value: string;
  onChange: (value: string) => void;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'onChange' | 'value'>) {
  return (
    <Input
      {...props}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="h-10 px-4"
    />
  );
}

function CountryCombobox({
  value,
  onChange
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Select<string>
      value={value || null}
      items={countryOptions}
      onValueChange={(nextValue) => {
        if (nextValue !== null) {
          onChange(nextValue);
        }
      }}
    >
      <SelectTrigger
        aria-label="Country"
        className="h-10 w-full px-4"
      >
        <SelectValue placeholder="Select" />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false} className="min-w-[208px]">
        {countryOptions.map((option) => (
          <SelectItem key={option.value} value={option.value} label={option.label}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function formFromOrganization(organization: Organization, profile: OrganizationProfile): FormState {
  const address = profile.physical_address;
  return {
    name: organization.name ?? '',
    line1: address?.line1 ?? '',
    line2: address?.line2 ?? '',
    country: address?.country ?? '',
    state: address?.state ?? '',
    city: address?.city ?? '',
    postalCode: address?.postal_code ?? ''
  };
}

function normalizeForm(form: FormState): FormState {
  return {
    name: form.name.trim(),
    line1: form.line1.trim(),
    line2: form.line2.trim(),
    country: form.country.trim(),
    state: form.state.trim(),
    city: form.city.trim(),
    postalCode: form.postalCode.trim()
  };
}

function sameForm(left: FormState, right: FormState) {
  return (
    left.name === right.name &&
    left.line1 === right.line1 &&
    left.line2 === right.line2 &&
    left.country === right.country &&
    left.state === right.state &&
    left.city === right.city &&
    left.postalCode === right.postalCode
  );
}

function isAddressValid(form: FormState) {
  const hasAnyAddressValue = Boolean(
    form.line1 || form.line2 || form.country || form.state || form.city || form.postalCode
  );
  if (!hasAnyAddressValue) {
    return true;
  }
  return Boolean(form.line1 && form.country && form.state && form.city && form.postalCode);
}

function addressPayload(form: FormState): OrganizationPhysicalAddress | null {
  if (!form.line1 && !form.line2 && !form.country && !form.state && !form.city && !form.postalCode) {
    return null;
  }
  return {
    line1: form.line1,
    line2: form.line2 || null,
    country: form.country,
    state: form.state,
    city: form.city,
    postal_code: form.postalCode
  };
}

function sameAddressPayload(left: OrganizationPhysicalAddress | null, right: OrganizationPhysicalAddress | null) {
  return JSON.stringify(left) === JSON.stringify(right);
}

function organizationAllowsApiKeys(organization: Organization) {
  return organization.settings?.default_workspace_settings?.enable_api_keys !== false;
}

function errorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Something went wrong. Try again.';
}
