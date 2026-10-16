import { useI18n } from '../../shared/i18n';
import { Shield } from 'lucide-react';
import { useContext } from 'react';
import { AuthContext } from '../../shared/auth/context';
import { DropdownMenuItem } from '../../shared/ui/dropdown-menu';

export function PlatformAdminMenuItem({ onNavigate }: { onNavigate: (path: string) => Promise<void> }) {
  const { msg } = useI18n();

  const platformAccess = useContext(AuthContext)?.platformAccess;
  if (!platformAccess?.permissions.includes('platform:access')) return null;
  return (
    <DropdownMenuItem className="h-8 gap-3 px-3" onClick={() => void onNavigate('/platform-admin')}>
      <Shield className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      {msg('platformAdmin.platformAdministration', 'Platform administration')}
    </DropdownMenuItem>
  );
}
