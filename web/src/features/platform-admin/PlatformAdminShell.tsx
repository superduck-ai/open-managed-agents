import { useLocale, localeLabels } from '../../shared/i18n';
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '../../shared/ui/select';
import { useI18n } from '../../shared/i18n';
import { ArrowLeft, Building2, ShieldCheck } from 'lucide-react';
import { Link, Outlet, useParams, useSearch } from '@tanstack/react-router';
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
  SidebarTrigger,
  useSidebar,
} from '../../shared/ui/sidebar';

export function PlatformAdminShell({ email }: { email: string }) {
  const { msg } = useI18n();

  return (
    <SidebarProvider open>
      <AdminSidebar email={email} />
      <SidebarInset className="min-h-screen min-w-0 text-foreground">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur-sm md:hidden">
          <SidebarTrigger aria-label={msg('platformAdmin.toggleAdministrationMenu', 'Toggle administration menu')} />
          <span className="text-sm font-semibold">
            {msg('platformAdmin.platformAdministration', 'Platform administration')}
          </span>
        </header>
        <main className="w-full min-w-0 px-6 py-6 lg:px-8">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}

function AdminSidebar({ email }: { email: string }) {
  const { msg } = useI18n();

  const { setOpenMobile } = useSidebar();
  const { locale, setLocale, supportedLocales } = useLocale();
  const { organizationUuid } = useParams({ strict: false });
  const search = useSearch({ strict: false });
  const view = organizationSection(search.view);
  const closeMenu = () => setOpenMobile(false);
  return (
    <Sidebar collapsible="offcanvas" className="border-r border-sidebar-border">
      <SidebarHeader className="p-4">
        <Link to="/platform-admin" onClick={closeMenu} className="flex items-center gap-3 rounded-md text-sm">
          <span className="grid size-8 shrink-0 place-items-center rounded-md border border-sidebar-border bg-sidebar-accent/40">
            <ShieldCheck className="size-4" aria-hidden />
          </span>
          <span className="font-semibold">
            {msg('platformAdmin.platformAdministration', 'Platform administration')}
          </span>
        </Link>
      </SidebarHeader>
      <SidebarContent className="px-2 py-2">
        <SidebarGroup className="p-0">
          <SidebarGroupLabel>{msg('platformAdmin.globalAdministration', 'Global administration')}</SidebarGroupLabel>
          <SidebarGroupContent>
            <nav aria-label={msg('platformAdmin.administrationNavigation', 'Administration navigation')}>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton
                    isActive={!organizationUuid}
                    render={
                      <Link
                        to="/platform-admin"
                        onClick={closeMenu}
                        aria-current={!organizationUuid ? 'page' : undefined}
                      />
                    }
                  >
                    <>{organizationUuid ? <ArrowLeft aria-hidden /> : <Building2 aria-hidden />}</>
                    <span>
                      {organizationUuid
                        ? msg('platformAdmin.backToOrganizations', 'Back to organizations')
                        : msg('platformAdmin.organizations', 'Organizations')}
                    </span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              </SidebarMenu>
            </nav>
          </SidebarGroupContent>
        </SidebarGroup>
        {organizationUuid && (
          <SidebarGroup className="mt-4 p-0">
            <SidebarGroupLabel>{msg('platformAdmin.currentOrganization', 'Current organization')}</SidebarGroupLabel>
            <SidebarGroupContent>
              <nav aria-label={msg('platformAdmin.organizationNavigation', 'Organization navigation')}>
                <SidebarMenu>
                  {organizationSections.map((section) => {
                    const Icon = section.icon;
                    const active = view === section.value;
                    return (
                      <SidebarMenuItem key={section.value}>
                        <SidebarMenuButton
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
                          <span>{msg(section.labelId, section.label)}</span>
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
      <SidebarFooter className="gap-3 border-t border-sidebar-border p-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton render={<Link to="/" />}>
              <ArrowLeft aria-hidden />
              <span>{msg('platformAdmin.backToConsole', 'Back to console')}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <Select
          value={locale}
          onValueChange={(next) => {
            if (next) setLocale(next);
          }}
        >
          <SelectTrigger className="w-full" aria-label={msg('language.label', 'Language')}>
            <SelectValue>{localeLabels[locale]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {supportedLocales.map((language) => (
              <SelectItem key={language} value={language}>
                {localeLabels[language]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="min-w-0 px-2 pb-1">
          <p className="truncate text-sm" title={email}>
            {email}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {msg('platformAdmin.superAdministrator', 'Super administrator')}
          </p>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
