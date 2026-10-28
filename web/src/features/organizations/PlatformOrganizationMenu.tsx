import { ArrowLeft } from 'lucide-react';
import { useNavigate } from '@tanstack/react-router';
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
  const navigate = useNavigate();
  const exit = async () => {
    const targetOrganizationUuid = organizations?.platformScope?.targetOrganizationUuid;
    if (!(await organizations?.exitOrganization?.())) return;
    if (targetOrganizationUuid)
      await navigate({
        to: '/platform-admin/organizations/$organizationUuid',
        params: { organizationUuid: targetOrganizationUuid },
      });
    else await navigate({ to: '/platform-admin' });
  };
  if (!organizations?.platformScope) return null;
  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel className="truncate py-1 pr-3 pl-10 text-xs">
          {msg('platformScope.active', 'Platform administrator · {organization}', {
            organization: organizationAccess?.organization.name ?? organizations.platformScope.targetOrganizationUuid,
          })}
        </DropdownMenuLabel>
        <DropdownMenuItem
          className="h-8 gap-3 pr-3 pl-10"
          disabled={organizations.switching}
          closeOnClick={false}
          onClick={() => confirmation.request(() => void exit())}
        >
          <ArrowLeft className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <span className="min-w-0 flex-1 truncate">
            {msg('platformScope.exit', 'Return to platform administration')}
          </span>
        </DropdownMenuItem>
      </DropdownMenuGroup>
      {confirmation.dialog}
    </>
  );
}
