import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Building2, Mail } from 'lucide-react';
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
import { useScopeConfirmation } from './useScopeConfirmation';

export function OrganizationMenu() {
  const organizations = useOrganizations();
  return organizations ? <ConnectedOrganizationMenu organizations={organizations} /> : null;
}

function ConnectedOrganizationMenu({ organizations }: { organizations: OrganizationContextValue }) {
  const { account } = useAuth();
  const [open, setOpen] = useState(false);
  const confirmation = useScopeConfirmation();
  const invitations = useQuery({ queryKey: ['invitations', account?.uuid], queryFn: listInvitations, retry: false });
  const enter = (orgUuid: string) =>
    confirmation.request(() => {
      setOpen(false);
      void organizations.switchOrganization(orgUuid);
    });
  return (
    <>
      <DropdownMenuRadioGroup value={organizations.orgUuid ?? ''}>
        {organizations.memberships.map((membership) => {
          const org = membership.organization;
          if (!org?.uuid) return null;
          return (
            <DropdownMenuRadioItem
              key={org.uuid}
              value={org.uuid}
              closeOnClick={false}
              onClick={() => {
                if (org.uuid !== organizations.orgUuid) enter(org.uuid!);
              }}
            >
              <Building2 className="size-4 shrink-0" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{org.name || org.uuid}</span>
                <span className="block text-xs text-muted-foreground">{organizationRoleLabel(membership.role)}</span>
              </span>
            </DropdownMenuRadioItem>
          );
        })}
      </DropdownMenuRadioGroup>
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
