import { useContext } from 'react';
import { BookOpen, Menu } from 'lucide-react';
import { Button, ButtonLink } from '../../shared/ui/button';
import { useSidebar } from '../../shared/ui/sidebar';
import { useI18n } from '../../shared/i18n';
import { AuthContext } from '../../shared/auth/context';
import { ConsoleSearch } from '../../features/console-search/ConsoleSearch';
import { InvitationNotifications } from '../../features/organizations/InvitationNotifications';

export function ConsoleTopbar({
  currentPath,
  onNavigate,
}: {
  currentPath: string;
  onNavigate?: (href: string) => void | Promise<void>;
}) {
  const { msg, locale } = useI18n();
  const auth = useContext(AuthContext);
  const platformAdmin = currentPath.startsWith('/platform-admin');
  const homeHref = '/dashboard';
  const { isMobile, toggleSidebar } = useSidebar();
  return (
    <header className="console-topbar">
      <div className="flex min-w-0 items-center gap-2">
        {isMobile && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={toggleSidebar}
            aria-label={msg('common.toggleSidebar', 'Toggle Sidebar')}
          >
            <Menu className="size-4" />
          </Button>
        )}
        <a
          className="console-brand-link"
          href={homeHref}
          aria-label="Open Managed Agents"
          onClick={(event) => {
            if (onNavigate) {
              event.preventDefault();
              void onNavigate(homeHref);
            }
          }}
        >
          <span aria-hidden className="console-brand-wordmark shrink-0" />
        </a>
      </div>
      <ConsoleSearch currentPath={currentPath} onNavigate={onNavigate} />
      <div className="flex items-center justify-end gap-2">
        <ButtonLink
          variant="ghost"
          size="sm"
          href={`https://oma.mintlifysite.com/docs/${locale === 'zh-CN' ? 'zh' : 'en'}/overview`}
          target="_blank"
          rel="noreferrer"
          aria-label={msg('nav.documentation', 'Documentation')}
        >
          <BookOpen className="size-4" />
          <span className="hidden lg:inline">{msg('nav.documentation', 'Documentation')}</span>
        </ButtonLink>
        {auth && !platformAdmin && <InvitationNotifications />}
      </div>
    </header>
  );
}
