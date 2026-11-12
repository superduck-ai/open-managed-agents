import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { ArrowRightLeft } from 'lucide-react';
import { useOrganizations, type OrganizationContextValue } from '../../shared/organizations/context';
import { DropdownMenuItem, DropdownMenuRadioGroup, DropdownMenuRadioItem } from '../../shared/ui/dropdown-menu';
import { organizationRoleLabel } from './api';
import { menuOrganizationPreview, organizationNameParts, type OrganizationPickerItem } from './organizationPicker';
import { OrganizationSwitcher } from './OrganizationSwitcher';
import { useScopeConfirmation } from './useScopeConfirmation';

export function OrganizationMenu() {
  const organizations = useOrganizations();
  return organizations ? <ConnectedOrganizationMenu organizations={organizations} /> : null;
}

function pickerItems(organizations: OrganizationContextValue): OrganizationPickerItem[] {
  return organizations.memberships.flatMap((membership) => {
    const uuid = membership.organization?.uuid;
    if (!uuid) return [];
    return [{ uuid, name: membership.organization?.name || uuid, role: membership.role }];
  });
}

function ConnectedOrganizationMenu({ organizations }: { organizations: OrganizationContextValue }) {
  const { msg } = useI18n();
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const confirmation = useScopeConfirmation();
  const items = pickerItems(organizations);
  const preview = menuOrganizationPreview(items, organizations.orgUuid);
  const enter = (orgUuid: string) =>
    confirmation.request(() => {
      setSwitcherOpen(false);
      void organizations.switchOrganization(orgUuid);
    });
  return (
    <>
      <DropdownMenuRadioGroup value={organizations.orgUuid ?? ''}>
        {preview.map((item) => {
          const parts = organizationNameParts(item.name);
          return (
            <DropdownMenuRadioItem
              key={item.uuid}
              value={item.uuid}
              closeOnClick={false}
              disabled={organizations.switching}
              onClick={() => {
                if (item.uuid !== organizations.orgUuid) enter(item.uuid);
              }}
            >
              <span className="min-w-0 flex-1">
                <span className="block truncate">{parts.title}</span>
                {parts.detail ? (
                  <span className="block truncate text-xs text-muted-foreground">{parts.detail}</span>
                ) : null}
                <span className="block text-xs text-muted-foreground">{organizationRoleLabel(item.role, msg)}</span>
              </span>
            </DropdownMenuRadioItem>
          );
        })}
      </DropdownMenuRadioGroup>
      {items.length > preview.length ? (
        <DropdownMenuItem closeOnClick={false} disabled={organizations.switching} onClick={() => setSwitcherOpen(true)}>
          <ArrowRightLeft className="size-4" />
          {msg('organizations.switch', 'Switch organization')}
        </DropdownMenuItem>
      ) : null}
      <OrganizationSwitcher
        open={switcherOpen}
        onOpenChange={setSwitcherOpen}
        items={items}
        currentOrgUuid={organizations.orgUuid}
        switching={organizations.switching}
        onSelect={enter}
      />
      {confirmation.dialog}
    </>
  );
}
