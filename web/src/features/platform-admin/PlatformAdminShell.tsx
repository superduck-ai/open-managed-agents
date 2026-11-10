import { useI18n } from '../../shared/i18n';
import type { CSSProperties } from 'react';
import { BrandLogo } from '../../shared/brand/BrandLogo';
import { productIcons } from '../../shared/icons/product-icons';
import '../../app/layout/console-navigation.css';
import { ArrowLeft, Blocks, Building2, KeyRound, ShieldCheck } from 'lucide-react';
import { Link, Outlet, useLocation, useParams, useSearch } from '@tanstack/react-router';
import { organizationSection, organizationSections } from './organizationNavigation';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from '../../shared/ui/sidebar';

export function PlatformAdminShell({ email }: { email: string }) {
  const { msg } = useI18n();

  return (
    <SidebarProvider
      defaultOpen
      className="console-shell"
      style={{ '--sidebar-width': 'var(--console-sidebar-width)', '--sidebar-width-icon': '64px' } as CSSProperties}
    >
      <AdminSidebar email={email} />
      <SidebarInset className="min-h-screen min-w-0 text-foreground">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b-[0.5px] bg-background/95 px-4 backdrop-blur-sm md:hidden">
          <SidebarTrigger aria-label={msg('platformAdmin.toggleAdministrationMenu', 'Toggle administration menu')} />
          <span className="text-sm font-semibold">
            {msg('platformAdmin.platformAdministration', 'Platform administration')}
          </span>
        </header>
        <div className="w-full min-w-0 px-6 py-6 lg:px-8">
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function AdminSidebar({ email }: { email: string }) {
  const { msg } = useI18n();

  const { setOpenMobile, state, isMobile } = useSidebar();
  const collapsed = state === 'collapsed' && !isMobile;
  const { organizationUuid } = useParams({ strict: false });
  const search = useSearch({ strict: false });
  const administratorsActive = useLocation().pathname === '/platform-admin/administrators';
  const skillsActive = useLocation().pathname === '/platform-admin/skills';
  const oauthActive = useLocation().pathname === '/platform-admin/oauth';
  const organizationsLabel = organizationUuid
    ? msg('platformAdmin.backToOrganizations', 'Back to organizations')
    : msg('platformAdmin.organizations', 'Organizations');
  const view = organizationSection(search.view);
  const closeMenu = () => setOpenMobile(false);
  return (
    <Sidebar collapsible="icon" className="border-r-[0.5px] border-sidebar-border">
      <AdminSidebarHeader closeMenu={closeMenu} />
      <SidebarContent className="console-sidebar-content scrollbar-none px-2 py-2" data-sidebar-scroll-area="true">
        <SidebarGroup className="p-0">
          {!organizationUuid && (
            <SidebarGroupLabel className="console-navigation-section-label">
              {msg('platformAdmin.globalAdministration', 'Global administration')}
            </SidebarGroupLabel>
          )}
          <SidebarGroupContent>
            <nav
              data-sidebar-navigation="platform"
              aria-label={msg('platformAdmin.administrationNavigation', 'Administration navigation')}
            >
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton
                    tooltip={organizationsLabel}
                    aria-label={organizationsLabel}
                    isActive={!organizationUuid && !administratorsActive && !skillsActive && !oauthActive}
                    render={
                      <Link
                        to="/platform-admin"
                        activeOptions={{ exact: true }}
                        onClick={closeMenu}
                        aria-current={
                          !organizationUuid && !administratorsActive && !skillsActive && !oauthActive
                            ? 'page'
                            : undefined
                        }
                      />
                    }
                  >
                    {organizationUuid ? <ArrowLeft aria-hidden /> : <Building2 aria-hidden />}
                    <span className="truncate group-data-[collapsible=icon]:hidden">{organizationsLabel}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                {!organizationUuid && (
                  <>
                    <SidebarMenuItem>
                      <SidebarMenuButton
                        tooltip={msg('platformAdmin.superAdministrators', 'Super administrators')}
                        aria-label={msg('platformAdmin.superAdministrators', 'Super administrators')}
                        isActive={administratorsActive}
                        render={
                          <Link
                            to="/platform-admin/administrators"
                            onClick={closeMenu}
                            aria-current={administratorsActive ? 'page' : undefined}
                          />
                        }
                      >
                        <ShieldCheck aria-hidden />
                        <span className="truncate group-data-[collapsible=icon]:hidden">
                          {msg('platformAdmin.superAdministrators', 'Super administrators')}
                        </span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                    <SidebarMenuItem>
                      <SidebarMenuButton
                        tooltip={msg('systemSkills.title', 'System skills')}
                        aria-label={msg('systemSkills.title', 'System skills')}
                        isActive={skillsActive}
                        render={
                          <Link
                            to="/platform-admin/skills"
                            onClick={closeMenu}
                            aria-current={skillsActive ? 'page' : undefined}
                          />
                        }
                      >
                        <Blocks aria-hidden />
                        <span className="truncate group-data-[collapsible=icon]:hidden">
                          {msg('systemSkills.title', 'System skills')}
                        </span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                    <SidebarMenuItem>
                      <SidebarMenuButton
                        tooltip={msg('platformAdmin.oauthManagement.title', 'OAuth management')}
                        aria-label={msg('platformAdmin.oauthManagement.title', 'OAuth management')}
                        isActive={oauthActive}
                        render={
                          <Link
                            to="/platform-admin/oauth"
                            onClick={closeMenu}
                            aria-current={oauthActive ? 'page' : undefined}
                          />
                        }
                      >
                        <KeyRound aria-hidden />
                        <span className="truncate group-data-[collapsible=icon]:hidden">
                          {msg('platformAdmin.oauthManagement.title', 'OAuth management')}
                        </span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  </>
                )}
              </SidebarMenu>
            </nav>
          </SidebarGroupContent>
        </SidebarGroup>
        {organizationUuid && (
          <SidebarGroup className="mt-2 p-0">
            <SidebarGroupLabel className="console-navigation-section-label">
              {msg('platformAdmin.currentOrganization', 'Current organization')}
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <nav
                data-sidebar-navigation="platform"
                aria-label={msg('platformAdmin.organizationNavigation', 'Organization navigation')}
              >
                <SidebarMenu>
                  {organizationSections.map((section) => {
                    const Icon = section.icon;
                    const active = view === section.value;
                    return (
                      <SidebarMenuItem key={section.value}>
                        <SidebarMenuButton
                          tooltip={msg(section.labelId, section.label)}
                          aria-label={msg(section.labelId, section.label)}
                          isActive={active}
                          render={
                            <Link
                              to="/platform-admin/organizations/$organizationUuid"
                              params={{ organizationUuid }}
                              search={{ view: section.value }}
                              onClick={closeMenu}
                              aria-current={active ? 'page' : undefined}
                            />
                          }
                        >
                          <Icon aria-hidden />
                          <span className="truncate group-data-[collapsible=icon]:hidden">
                            {msg(section.labelId, section.label)}
                          </span>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              </nav>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>
      <SidebarFooter className="mt-auto px-2 py-2">
        <SidebarMenu data-console-footer-links>
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip={msg('platformAdmin.backToConsole', 'Back to console')}
              aria-label={msg('platformAdmin.backToConsole', 'Back to console')}
              render={<Link to="/" onClick={closeMenu} />}
            >
              <ArrowLeft aria-hidden />
              <span className="truncate group-data-[collapsible=icon]:hidden">
                {msg('platformAdmin.backToConsole', 'Back to console')}
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <div
          className={`flex h-12 min-w-0 items-center gap-2 rounded-md p-2 ${collapsed ? 'justify-center' : ''}`}
          title={email}
        >
          <span className="grid size-8 shrink-0 place-items-center rounded-md border-[0.5px] border-sidebar-border text-sidebar-foreground">
            <ShieldCheck className="size-4" strokeWidth={1.5} aria-hidden />
          </span>
          {!collapsed && (
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm" title={email}>
                {email}
              </p>
              <p className="text-xs text-muted-foreground">
                {msg('platformAdmin.superAdministrator', 'Super administrator')}
              </p>
            </div>
          )}
        </div>
      </SidebarFooter>
      <SidebarRail
        aria-label={msg('common.toggleSidebar', 'Toggle Sidebar')}
        title={msg('common.toggleSidebar', 'Toggle Sidebar')}
      />
    </Sidebar>
  );
}

function AdminSidebarHeader({ closeMenu }: { closeMenu: () => void }) {
  const { msg } = useI18n();
  const { state, isMobile } = useSidebar();
  const collapsed = state === 'collapsed' && !isMobile;
  const title = msg('platformAdmin.platformAdministration', 'Platform administration');
  const toggleLabel = collapsed ? msg('common.expand', 'Expand') : msg('common.collapse', 'Collapse');
  return (
    <SidebarHeader className="console-sidebar-header" data-console-compact={collapsed ? 'true' : undefined}>
      <div className={`flex h-12 items-center ${collapsed ? 'justify-center' : 'justify-between'}`}>
        {!collapsed && (
          <Link
            to="/platform-admin"
            onClick={closeMenu}
            aria-label="Open Managed Agents"
            className="flex h-12 min-w-0 flex-1 items-center rounded-md px-2 outline-none focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-ring"
          >
            <BrandLogo decorative className="h-7 w-[83px]" />
          </Link>
        )}
        {!isMobile && (
          <SidebarTrigger aria-label={toggleLabel} aria-expanded={!collapsed} title={toggleLabel}>
            <productIcons.sidebar className="size-4" aria-hidden />
          </SidebarTrigger>
        )}
      </div>
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton
            size="lg"
            tooltip={title}
            render={<Link to="/platform-admin" onClick={closeMenu} />}
            aria-label={collapsed ? title : undefined}
          >
            <span className="grid size-8 shrink-0 place-items-center text-sidebar-foreground">
              <ShieldCheck className="size-4" strokeWidth={1.5} aria-hidden />
            </span>
            {!collapsed && <span className="truncate text-sm font-semibold">{title}</span>}
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarHeader>
  );
}
