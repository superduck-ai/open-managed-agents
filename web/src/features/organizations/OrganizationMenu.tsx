import { useI18n } from '../../shared/i18n';
import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ArrowRightLeft, Mail } from 'lucide-react';
import { useAuth } from '../../shared/auth/context';
import { useOrganizations, type OrganizationContextValue } from '../../shared/organizations/context';
import {
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
} from '../../shared/ui/dropdown-menu';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { organizationRoleLabel, listInvitations } from './api';
import { InvitationList } from './InvitationList';
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
  const { account } = useAuth();
  const [open, setOpen] = useState(false);
  const invitationDialogRef = useRef<HTMLDivElement>(null);
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const confirmation = useScopeConfirmation();
  const invitations = useQuery({ queryKey: ['invitations', account?.uuid], queryFn: listInvitations, retry: false });
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
      <DropdownMenuSeparator />
      <DropdownMenuItem
        closeOnClick={false}
        onClick={() => {
          setOpen(true);
          void invitations.refetch();
        }}
      >
        <Mail className="size-4" />
        {invitations.data
          ? msg('invitations.menu.count', 'Organization invitations ({count, number})', {
              count: invitations.data.data.length,
            })
          : invitations.isError
            ? msg('invitations.menu.error', 'Organization invitations (could not load)')
            : msg('invitations.title', 'Organization invitations')}
      </DropdownMenuItem>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          ref={invitationDialogRef}
          initialFocus={invitationDialogRef}
          className="grid-rows-[auto_minmax(0,1fr)] max-h-[calc(100dvh-2rem)] overflow-clip sm:max-w-lg"
        >
          <DialogHeader>
            <DialogTitle>{msg('invitations.title', 'Organization invitations')}</DialogTitle>
            <DialogDescription>
              {msg(
                'invitations.stayInOrganization',
                'Accepting an invitation keeps you in the current organization. You can switch organizations later.',
              )}
            </DialogDescription>
          </DialogHeader>
          <InvitationList
            invitations={invitations.data?.data}
            error={invitations.error}
            loading={invitations.isLoading}
            retry={() => void invitations.refetch()}
          />
        </DialogContent>
      </Dialog>
      {confirmation.dialog}
    </>
  );
}
