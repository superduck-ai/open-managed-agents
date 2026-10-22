import { UserRound } from 'lucide-react';
import { useMemo } from 'react';
import { formatRole } from '../dashboard/model';
import { useAuth } from '../../shared/auth/context';
import { useI18n, useLocale, localeLabels, type Locale } from '../../shared/i18n';
import { themeModes, useTheme, type ThemeMode } from '../../shared/theme/context';
import { Badge } from '../../shared/ui/badge';
import { Card, CardContent, CardHeader } from '../../shared/ui/card';
import { Field, FieldDescription, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';

export const settingsSections = [
  'profile',
  'appearance',
  'organization',
  'llm',
  'members',
  'workspaces',
  'api-keys',
] as const;
export type SettingsPageSection = (typeof settingsSections)[number] | 'retired';

export function settingsSectionFromPath(pathname: string): SettingsPageSection {
  const section = pathname.match(/^\/settings\/([^/]+)/)?.[1] ?? 'organization';
  return settingsSections.find((candidate) => candidate === section) ?? 'retired';
}

export function ProfileSettingsPage() {
  const { account } = useAuth();
  const { msg } = useI18n();
  const membership = account?.memberships?.[0];
  const displayName =
    account?.display_name ||
    account?.full_name ||
    account?.email_address?.split('@')[0] ||
    msg('settings.profile.unknownName', 'Unknown user');
  const organizationName =
    membership?.organization?.name || msg('settings.profile.unknownOrganization', 'Unknown organization');
  const role = formatRole(membership?.role, msg);

  return (
    <section className="mx-auto w-full max-w-[1100px]" data-testid="settings-profile-page">
      <Card>
        <CardHeader className="space-y-3">
          <div className="flex items-center gap-3">
            <Badge variant="secondary" className="rounded-full px-2.5 py-1">
              <UserRound className="size-3.5" aria-hidden />
              {msg('nav.profile', 'Profile')}
            </Badge>
          </div>
          <div className="space-y-1">
            <h1 className="text-xl font-semibold tracking-normal text-foreground">{msg('nav.profile', 'Profile')}</h1>
            <p className="text-sm text-muted-foreground">
              {msg(
                'settings.profile.description',
                'Review the account identity and organization role currently active in this session.',
              )}
            </p>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <ReadonlyField label={msg('settings.profile.displayName', 'Display name')} value={displayName} />
          <ReadonlyField
            label={msg('settings.profile.emailAddress', 'Email address')}
            value={account?.email_address || '—'}
          />
          <ReadonlyField label={msg('settings.profile.accountId', 'Account ID')} value={account?.uuid || '—'} />
          <ReadonlyField label={msg('settings.profile.organization', 'Organization')} value={organizationName} />
          <ReadonlyField label={msg('settings.profile.organizationRole', 'Organization role')} value={role} />
        </CardContent>
      </Card>
    </section>
  );
}

export function AppearanceSettingsPage() {
  const { msg } = useI18n();
  const { locale, setLocale } = useLocale();
  const { mode, setMode } = useTheme();
  const languageOptions = useMemo(
    () => settingsLocaleOptions.map((option) => ({ ...option, label: localeLabels[option.value] })),
    [],
  );
  const themeOptions = useMemo(
    () =>
      themeModes.map((value) => ({
        value,
        label:
          value === 'system'
            ? msg('theme.system', 'System')
            : value === 'light'
              ? msg('theme.light', 'Light')
              : msg('theme.dark', 'Dark'),
      })),
    [msg],
  );

  return (
    <section className="mx-auto w-full max-w-[1100px] space-y-4" data-testid="settings-appearance-page">
      <Card>
        <CardHeader className="space-y-1">
          <h1 className="text-xl font-semibold tracking-normal text-foreground">
            {msg('nav.appearance', 'Appearance')}
          </h1>
          <p className="text-sm text-muted-foreground">
            {msg('settings.appearance.description', 'Customize language and theme preferences for the console.')}
          </p>
        </CardHeader>
        <CardContent className="space-y-6">
          <Field className="gap-2">
            <FieldLabel htmlFor="settings-language">{msg('language.label', 'Language')}</FieldLabel>
            <Select<Locale>
              value={locale}
              items={languageOptions}
              onValueChange={(nextValue) => {
                if (nextValue !== null) {
                  setLocale(nextValue);
                }
              }}
            >
              <SelectTrigger id="settings-language" aria-label={msg('language.label', 'Language')} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {languageOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {msg('settings.appearance.languageHelp', 'Changes the language used across the console immediately.')}
            </FieldDescription>
          </Field>

          <Field className="gap-2">
            <FieldLabel htmlFor="settings-theme">{msg('theme.label', 'Theme')}</FieldLabel>
            <Select<ThemeMode>
              value={mode}
              items={themeOptions}
              onValueChange={(nextValue) => {
                if (nextValue !== null) {
                  setMode(nextValue);
                }
              }}
            >
              <SelectTrigger id="settings-theme" aria-label={msg('theme.label', 'Theme')} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {themeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {msg(
                'settings.appearance.themeHelp',
                'Theme preference applies immediately and is saved for future sessions.',
              )}
            </FieldDescription>
          </Field>
        </CardContent>
      </Card>
    </section>
  );
}

function ReadonlyField({ label, value }: { label: string; value: string }) {
  return (
    <Field className="gap-2">
      <FieldLabel>{label}</FieldLabel>
      <Input value={value} readOnly aria-label={label} aria-readonly="true" />
    </Field>
  );
}

const settingsLocaleOptions = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
] satisfies Array<{ value: Locale; label: string }>;
