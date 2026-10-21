import { usePlatformAdminError } from './usePlatformAdminError';
import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import type { ApiError } from '../../shared/api/client';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { usePlatformAdmin } from './context';
import type { Organization } from './types';

export function OrganizationProfileForm({
  organization,
  onSaved,
  onCancel,
}: {
  organization: Organization;
  onSaved: (organization: Organization) => void;
  onCancel: () => void;
}) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();

  const { client } = usePlatformAdmin();
  const [snapshot] = useState(organization);
  const [name, setName] = useState(organization.name);
  const [website, setWebsite] = useState(organization.website);
  const [industry, setIndustry] = useState(organization.industry);
  const mutation = useMutation({
    mutationFn: () =>
      client.request<Organization>(`/organizations/${snapshot.uuid}`, {
        method: 'PATCH',
        body: {
          name: name.trim(),
          website: website.trim(),
          industry: industry.trim(),
          expected_version: snapshot.version,
        },
      }),
    onSuccess: onSaved,
  });
  return (
    <form
      className="max-w-xl space-y-4 rounded-lg border p-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!mutation.isPending) mutation.mutate();
      }}
    >
      <h2 className="font-semibold">{msg('platformAdmin.editOrganizationDetails', 'Edit organization details')}</h2>
      <div className="space-y-2">
        <Label htmlFor="org-profile-name">{msg('platformAdmin.organizationName', 'Organization name')}</Label>
        <Input
          id="org-profile-name"
          required
          maxLength={200}
          value={name}
          disabled={mutation.isPending}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="org-profile-website">{msg('platformAdmin.website', 'Website')}</Label>
        <Input
          id="org-profile-website"
          maxLength={2000}
          value={website}
          disabled={mutation.isPending}
          onChange={(event) => setWebsite(event.target.value)}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="org-profile-industry">{msg('platformAdmin.industry', 'Industry')}</Label>
        <Input
          id="org-profile-industry"
          maxLength={200}
          value={industry}
          disabled={mutation.isPending}
          onChange={(event) => setIndustry(event.target.value)}
        />
      </div>
      {mutation.isError && (
        <p role="alert" className="text-destructive">
          {errorMessage(mutation.error)}{' '}
          {(mutation.error as unknown as ApiError).status === 409 &&
            msg(
              'platformAdmin.yourInputHasBeenPreservedReviewTheLatestDetails',
              'Your input has been preserved. Review the latest details before editing again.',
            )}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={mutation.isPending || !name.trim()}>
          {mutation.isPending
            ? msg('platformAdmin.saving', 'Saving…')
            : msg('platformAdmin.saveDetails', 'Save details')}
        </Button>
        <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onCancel}>
          {msg('platformAdmin.cancelEditing', 'Cancel editing')}
        </Button>
      </div>
    </form>
  );
}
