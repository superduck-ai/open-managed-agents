import { useI18n } from '../../shared/i18n';
import { ExternalLink, Shield } from 'lucide-react';
import { useContext } from 'react';
import { AuthContext } from '../../shared/auth/context';
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from '../../shared/ui/dropdown-menu';
import { PlatformOrganizationMenu } from '../organizations/PlatformOrganizationMenu';

export function PlatformAdminMenuItem({ onNavigate }: { onNavigate: (path: string) => Promise<void> }) {
  const { msg } = useI18n();

  const platformAccess = useContext(AuthContext)?.platformAccess;
  if (!platformAccess?.permissions.includes('platform:access')) return null;
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger openOnHover className="h-8 gap-3 px-3">
        <Shield className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="min-w-0 flex-1 truncate">
          {msg('platformAdmin.platformAdministration', 'Platform administration')}
        </span>
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-72 max-w-[calc(100vw-2rem)]">
        <DropdownMenuItem className="h-8 gap-3 px-3" onClick={() => void onNavigate('/platform-admin')}>
          <ExternalLink className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <span className="min-w-0 flex-1 truncate">
            {msg('platformAdmin.openPlatformAdministration', 'Open platform administration')}
          </span>
        </DropdownMenuItem>
        <PlatformOrganizationMenu />
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}
