import { ArrowLeft } from 'lucide-react';
import { useNavigate } from '@tanstack/react-router';
import { useOrganizations } from '../../shared/organizations/context';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { useScopeConfirmation } from './useScopeConfirmation';

export function PlatformReturnButton({ collapsed = false }: { collapsed?: boolean }) {
  const organizations = useOrganizations();
  return organizations?.platformScope ? <ConnectedPlatformReturnButton collapsed={collapsed} /> : null;
}

function ConnectedPlatformReturnButton({ collapsed }: { collapsed: boolean }) {
  const organizations = useOrganizations();
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
      <Button
        variant="secondary"
        className={`h-10 w-full gap-3 rounded-xl bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent ${collapsed ? 'justify-center px-2' : 'justify-start px-3'}`}
        aria-label={msg('platformScope.exit', 'Return to platform administration')}
        title={msg('platformScope.exit', 'Return to platform administration')}
        disabled={organizations.switching}
        onClick={() => confirmation.request(() => void exit())}
      >
        <ArrowLeft className="size-4 shrink-0" aria-hidden />
        {collapsed ? null : (
          <span className="min-w-0 truncate">{msg('platformScope.exit', 'Return to platform administration')}</span>
        )}
      </Button>
      {confirmation.dialog}
    </>
  );
}
