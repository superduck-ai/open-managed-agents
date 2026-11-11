import { useI18n } from '../../shared/i18n';
import type { CSSProperties } from 'react';
import { ConsoleTopbar } from '../console-topbar/ConsoleTopbar';
import { useResponsiveSidebar } from '../../shared/hooks/use-responsive-sidebar';
import '../../shared/ui/console-navigation.css';
import { ArrowLeft, Blocks, Building2, Shield, ShieldCheck } from 'lucide-react';
import { Link, Outlet, useLocation, useNavigate, useParams, useSearch } from '@tanstack/react-router';
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
  useSidebar,
} from '../../shared/ui/sidebar';

export function PlatformAdminShell({ email }: { email: string }) {
  const expanded = useResponsiveSidebar();
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <SidebarProvider
      open={expanded}
      onOpenChange={() => undefined}
      className="console-shell unified-console"
      style={{ '--sidebar-width': 'var(--console-sidebar-width)', '--sidebar-width-icon': '64px' } as CSSProperties}
    >
      <ConsoleTopbar currentPath={location.pathname} onNavigate={(href) => navigate({ href })} />
      <AdminSidebar email={email} />
      <SidebarInset className="subtle-scrollbar min-w-0 text-foreground">
        <div className="console-page w-full min-w-0 px-6 py-6 lg:px-8">
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
  const organizationsLabel = organizationUuid
    ? msg('platformAdmin.backToOrganizations', 'Back to organizations')
    : msg('platformAdmin.organizations', 'Organizations');
  const view = organizationSection(search.view);
  const closeMenu = () => setOpenMobile(false);
  return (
    <Sidebar collapsible="icon">
      <AdminSidebarHeader />
      <SidebarContent className="console-sidebar-content scrollbar-none px-2 py-2" data-sidebar-scroll-area="true">
        <SidebarGroup className="p-0">
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
                    isActive={!organizationUuid && !administratorsActive && !skillsActive}
                    render={
                      <Link
                        to="/platform-admin"
                        activeOptions={{ exact: true }}
                        onClick={closeMenu}
                        aria-current={!organizationUuid && !administratorsActive && !skillsActive ? 'page' : undefined}
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
    </Sidebar>
  );
}

function AdminSidebarHeader() {
  const { msg } = useI18n();
  const { state, isMobile } = useSidebar();
  const collapsed = state === 'collapsed' && !isMobile;
  const title = msg('platformAdmin.platformAdministration', 'Super admin console');
  return (
    <SidebarHeader className="console-sidebar-header" data-console-compact={collapsed ? 'true' : undefined}>
      <div
        className={`flex h-14 items-center gap-2.5 px-3 text-sidebar-foreground ${collapsed ? 'justify-center' : ''}`}
        title={title}
      >
        <Shield className="size-[18px] shrink-0" strokeWidth={1.5} aria-hidden />
        {!collapsed && <span className="truncate text-sm font-semibold">{title}</span>}
      </div>
    </SidebarHeader>
  );
}
