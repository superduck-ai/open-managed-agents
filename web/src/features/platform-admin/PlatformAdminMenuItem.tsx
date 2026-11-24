import { useI18n } from '../../shared/i18n';
import { Shield } from 'lucide-react';
import { useContext } from 'react';
import { AuthContext } from '../../shared/auth/context';
import { useOrganizations } from '../../shared/organizations/context';
import { useScopeConfirmation } from '../organizations/useScopeConfirmation';
import { DropdownMenuItem } from '../../shared/ui/dropdown-menu';

export function PlatformAdminMenuItem({ onNavigate }: { onNavigate: (path: string) => Promise<void> }) {
  const { msg } = useI18n();

  const organizations = useOrganizations();
  const confirmation = useScopeConfirmation();
  const platformAccess = useContext(AuthContext)?.platformAccess;
  if (!platformAccess?.permissions.includes('platform:access')) return null;
  const open = async () => {
    const target = organizations?.platformScope?.targetOrganizationUuid;
    if (organizations?.platformScope && !(await organizations.exitOrganization?.())) return;
    await onNavigate(target ? `/platform-admin/organizations/${encodeURIComponent(target)}` : '/platform-admin');
  };
  return (
    <>
      <DropdownMenuItem
        className="h-8 gap-3 px-3"
        closeOnClick={false}
        disabled={organizations?.switching}
        onClick={() => confirmation.request(() => void open())}
      >
        <Shield className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="min-w-0 flex-1 truncate">
          {msg('platformAdmin.platformAdministration', 'Super admin console')}
        </span>
      </DropdownMenuItem>
      {confirmation.dialog}
    </>
  );
}
