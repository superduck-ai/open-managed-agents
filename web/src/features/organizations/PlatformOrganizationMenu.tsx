import { useOrganizations } from '../../shared/organizations/context';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel } from '../../shared/ui/dropdown-menu';
import { useScopeConfirmation } from './useScopeConfirmation';

export function PlatformOrganizationMenu() {
  const organizations = useOrganizations();
  return organizations?.platformScope ? <ConnectedPlatformOrganizationMenu /> : null;
}

function ConnectedPlatformOrganizationMenu() {
  const organizations = useOrganizations();
  const { organizationAccess } = useAuth();
  const { msg } = useI18n();
  const confirmation = useScopeConfirmation();
  if (!organizations?.platformScope) return null;
  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel className="truncate px-3 py-2 text-xs">
          {msg('platformScope.active', 'Platform administrator · {organization}', {
            organization: organizationAccess?.organization.name ?? organizations.platformScope.targetOrganizationUuid,
          })}
        </DropdownMenuLabel>
        <DropdownMenuItem
          disabled={organizations.switching}
          closeOnClick={false}
          onClick={() => confirmation.request(() => void organizations.exitOrganization?.())}
        >
          {organizations.platformScope.returnOrganizationUuid
            ? msg('platformScope.exit', 'Exit and return to original organization')
            : msg('platformScope.exitEmpty', 'Exit organization console')}
        </DropdownMenuItem>
      </DropdownMenuGroup>
      {confirmation.dialog}
    </>
  );
}
