import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ArrowRightLeft, Mail } from 'lucide-react';
import { useAuth } from '../../shared/auth/context';
import { useOrganizations, type OrganizationContextValue } from '../../shared/organizations/context';
import { DropdownMenuItem, DropdownMenuSeparator } from '../../shared/ui/dropdown-menu';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { organizationRoleLabel, listInvitations } from './api';
import { InvitationList } from './InvitationList';
import { organizationNameParts, type OrganizationPickerItem } from './organizationPicker';
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
  const { account } = useAuth();
  const [open, setOpen] = useState(false);
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const confirmation = useScopeConfirmation();
  const invitations = useQuery({ queryKey: ['invitations', account?.uuid], queryFn: listInvitations, retry: false });
  const items = pickerItems(organizations);
  const current = items.find((item) => item.uuid === organizations.orgUuid);
  const currentParts = organizationNameParts(current?.name ?? '');
  const enter = (orgUuid: string) =>
    confirmation.request(() => {
      setSwitcherOpen(false);
      void organizations.switchOrganization(orgUuid);
    });
  return (
    <>
      {current ? (
        <div className="px-3 py-2">
          <span className="block truncate text-sm text-foreground">{currentParts.title}</span>
          {currentParts.detail ? (
            <span className="block truncate text-xs text-muted-foreground">{currentParts.detail}</span>
          ) : null}
          <span className="block text-xs text-muted-foreground">{organizationRoleLabel(current.role)}</span>
        </div>
      ) : null}
      <DropdownMenuItem closeOnClick={false} disabled={organizations.switching} onClick={() => setSwitcherOpen(true)}>
        <ArrowRightLeft className="size-4" />
        切换组织
      </DropdownMenuItem>
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
        组织邀请{invitations.data ? ` (${invitations.data.data.length})` : invitations.isError ? ' · 加载失败' : ''}
      </DropdownMenuItem>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>组织邀请</DialogTitle>
            <DialogDescription>接受邀请后仍留在当前组织，可随时选择进入新组织。</DialogDescription>
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
