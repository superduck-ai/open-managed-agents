import {
  navigationHref,
  isActivePath,
  isWideConsolePath,
  isPlainNavigationClick,
  isSessionDetailPath,
} from '../../shared/navigation/navigation-paths';
import { currentOrganizationName, isSettingsPath, settingScope } from '../../shared/navigation/settings-navigation';
import { SettingsNavigation, SettingsLocation, SettingsAccess, CompactNavigationGroup } from './SettingsNavigation';
import { useSettingsHistory } from './use-settings-navigation';
import { useResponsiveSidebar } from '../../shared/hooks/use-responsive-sidebar';
import { ConsoleTopbar } from '../../features/console-topbar/ConsoleTopbar';
import '../../shared/ui/console-navigation.css';
import { productIcons as navigationIcons } from '@/shared/icons/product-icons';
import { AppearanceMenuRow } from './AppearanceMenuRow';
import { workspaceSwitchPath } from '../../shared/workspaces/presentation';
import { OrganizationMenu } from '../../features/organizations/OrganizationMenu';
import { PlatformReturnButton } from '../../features/organizations/PlatformReturnButton';
import { PlatformAdminMenuItem } from '../../features/platform-admin/PlatformAdminMenuItem';
import { useScopeConfirmation } from '../../features/organizations/useScopeConfirmation';
import { useOrganizations } from '../../shared/organizations/context';
import { Building2, ChevronDown, Globe2, LogOut, Plus, Settings } from 'lucide-react';
import { Outlet, useLocation, useNavigate } from '@tanstack/react-router';
import {
  useCallback,
  useContext,
  useEffect,
  forwardRef,
  useState,
  type AnchorHTMLAttributes,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
} from 'react';
import clsx from 'clsx';
import { Collapsible, CollapsibleContent } from '@/shared/ui/collapsible';
import {
  Sidebar as AppSidebar,
  SidebarContent as AppSidebarContent,
  SidebarFooter as AppSidebarFooter,
  SidebarHeader as AppSidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  useSidebar,
} from '@/shared/ui/sidebar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/shared/ui/dropdown-menu';
import { AuthContext, useAuth } from '../../shared/auth/context';
import type { AuthAccount } from '../../shared/auth/api';
import { useWorkspace } from '../../shared/workspaces/context';
import type { Workspace } from '../../shared/workspaces/api';
import { CreateWorkspaceDialog } from '../../shared/workspaces/CreateWorkspaceDialog';
import { buildCreateWorkspaceInput, workspaceColor, workspaceIdFromPath } from '../../shared/workspaces/presentation';
import { localeLabels, useI18n, useLocale } from '../../shared/i18n';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import {
  consoleNavigation,
  sortedResourceNavigation,
  type NavGroupItem,
  type NavLinkItem,
} from '../../shared/navigation/navigation';

type ConsoleShellProps = {
  account?: AuthAccount | null;
  currentPath?: string;
  children: ReactNode;
  onLogout: () => Promise<void> | void;
  onNavigate?: NavigateHandler;
};

type NavigateHandler = (href: string) => Promise<void> | void;
type ShellLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & {
  href: string;
  onNavigate?: NavigateHandler;
};
const interactiveMotionClass = 'transition-colors duration-150 ease-out motion-reduce:transition-none';

const ShellLink = forwardRef<HTMLAnchorElement, ShellLinkProps>(function ShellLink(
  { href, onNavigate, onClick, target, children, ...props },
  ref,
) {
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (!onNavigate || target || !isPlainNavigationClick(event)) {
      return;
    }

    event.preventDefault();
    void onNavigate(href);
  };

  return (
    <a {...props} ref={ref} href={href} target={target} onClick={handleClick}>
      {children}
    </a>
  );
});

export function ConsoleLayout() {
  const { account, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const { activeWorkspaceId } = useWorkspace();
  const scoped = settingScope(location.pathname) === 'workspace';
  const handleNavigate = useCallback(
    async (href: string) => {
      await navigate({ href });
    },
    [navigate],
  );

  const handleLogout = async () => {
    await logout();
    await navigate({ to: '/login', search: { returnTo: '/' } });
  };

  return (
    <ConsoleShell account={account} currentPath={location.pathname} onLogout={handleLogout} onNavigate={handleNavigate}>
      <Outlet key={scoped ? activeWorkspaceId : undefined} />
    </ConsoleShell>
  );
}

export function ConsoleShell({ account, currentPath = '/', children, onLogout, onNavigate }: ConsoleShellProps) {
  const expanded = useResponsiveSidebar();
  const settings = isSettingsPath(currentPath);
  const quickstart = /^(?:\/quickstart|\/workspaces\/[^/]+\/agent-quickstart)\/?$/.test(currentPath);
  const sessionDetail = isSessionDetailPath(currentPath);
  return (
    <SidebarProvider
      open={expanded}
      onOpenChange={() => undefined}
      className="console-shell unified-console"
      style={{ '--sidebar-width': 'var(--console-sidebar-width)', '--sidebar-width-icon': '64px' } as CSSProperties}
    >
      <ConsoleTopbar currentPath={currentPath} onNavigate={onNavigate} />
      <ConsoleSidebar account={account} currentPath={currentPath} onLogout={onLogout} onNavigate={onNavigate} />
      <SidebarInset
        className={clsx(
          'subtle-scrollbar min-w-0 text-foreground',
          sessionDetail && 'session-route-theme console-session-inset',
          quickstart && 'console-quickstart-inset',
        )}
      >
        {settings && <SettingsLocation currentPath={currentPath} account={account} />}
        <div
          className={clsx(
            'console-page',
            sessionDetail
              ? 'console-session-content'
              : isWideConsolePath(currentPath) || settings
                ? 'px-6 py-6 lg:px-8'
                : 'mx-auto w-full max-w-[1440px] px-6 py-10',
            quickstart && 'console-quickstart-content',
          )}
        >
          <SettingsAccess currentPath={currentPath} account={account}>
            {children}
          </SettingsAccess>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function ConsoleSidebar({ account, currentPath = '/', onLogout, onNavigate }: Omit<ConsoleShellProps, 'children'>) {
  const { msg } = useI18n();
  const { activeWorkspaceId, orgUuid, selectWorkspace, workspaces } = useWorkspace();
  const { state, isMobile, setOpenMobile } = useSidebar();
  const collapsed = state === 'collapsed' && !isMobile;
  const settings = isSettingsPath(currentPath);
  const routeWorkspaceId = workspaceIdFromPath(currentPath);
  const history = useSettingsHistory(account, orgUuid, activeWorkspaceId, currentPath);
  useEffect(() => {
    if (
      routeWorkspaceId &&
      routeWorkspaceId !== activeWorkspaceId &&
      workspaces.some((workspace) => workspace.id === routeWorkspaceId)
    )
      selectWorkspace(routeWorkspaceId);
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace, workspaces]);
  const navigate: NavigateHandler = (href) => {
    setOpenMobile(false);
    if (onNavigate) return onNavigate(href);
    window.location.assign(href);
  };
  return (
    <AppSidebar collapsible="icon" data-sidebar-state={collapsed ? 'collapsed' : 'expanded'}>
      <ShellSidebarHeader currentPath={currentPath} onNavigate={navigate} />
      <AppSidebarContent className="console-sidebar-content scrollbar-none px-2 py-2" data-sidebar-scroll-area="true">
        {settings ? (
          <SettingsNavigation
            key={orgUuid}
            account={account}
            currentPath={currentPath}
            onNavigate={navigate}
            returnHref={history.returnHref}
          />
        ) : (
          <nav data-sidebar-navigation="main" aria-label={msg('nav.consoleNavigation', 'Console navigation')}>
            <SidebarMenu>
              {consoleNavigation.map((item) =>
                item.type === 'link' ? (
                  <SidebarLink
                    key={item.href}
                    collapsed={collapsed}
                    item={item}
                    currentPath={currentPath}
                    onNavigate={navigate}
                  />
                ) : (
                  <ResourceNavigation key={item.label} item={item} currentPath={currentPath} onNavigate={navigate} />
                ),
              )}
            </SidebarMenu>
          </nav>
        )}
      </AppSidebarContent>
      <SidebarFooter
        account={account}
        collapsed={collapsed}
        onLogout={onLogout}
        onNavigate={navigate}
        workspaceEntry={
          !settings && (
            <SidebarMenuItem>
              <SidebarMenuButton
                render={<ShellLink href={history.settingsHref} onNavigate={navigate} />}
                aria-label={msg('nav.settings', 'Settings')}
                tooltip={msg('nav.settings', 'Settings')}
              >
                <navigationIcons.settings className="size-4" aria-hidden />
                {!collapsed && <span>{msg('nav.settings', 'Settings')}</span>}
              </SidebarMenuButton>
            </SidebarMenuItem>
          )
        }
      />
    </AppSidebar>
  );
}

function ResourceNavigation({
  item,
  currentPath,
  onNavigate,
}: {
  item: NavGroupItem;
  currentPath: string;
  onNavigate?: NavigateHandler;
}) {
  const { msg, locale } = useI18n();
  const { state, isMobile } = useSidebar();
  const { activeWorkspaceId } = useWorkspace();
  const collapsed = state === 'collapsed' && !isMobile;
  const active = item.children.some((child) => isActivePath(currentPath, child.href));
  const [expanded, setExpanded] = useState(false);
  const children = sortedResourceNavigation(item.children, locale);
  const Icon = item.icon;
  if (collapsed)
    return (
      <CompactNavigationGroup
        label={msg(item.labelId, item.label)}
        icon={Icon}
        items={children}
        currentPath={currentPath}
        onNavigate={onNavigate}
      />
    );
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        type="button"
        isActive={active && !expanded}
        tooltip={msg(item.labelId, item.label)}
        aria-expanded={expanded}
        onClick={() => {
          setExpanded(!expanded);
        }}
      >
        <Icon className="size-4" aria-hidden />
        <span className="flex-1 truncate">{msg(item.labelId, item.label)}</span>
        <navigationIcons.chevronRight
          className={clsx(
            'size-3.5 transition-transform duration-150 motion-reduce:transition-none',
            expanded ? '-rotate-90' : 'rotate-90',
          )}
          aria-hidden
        />
      </SidebarMenuButton>
      <Collapsible open={expanded}>
        <CollapsibleContent>
          <SidebarMenuSub className="mx-0 translate-x-0 border-0 px-0" data-resource-navigation="true">
            {children.map((child) => {
              const ChildIcon = child.icon;
              return (
                <SidebarMenuSubItem key={child.href}>
                  <SidebarMenuSubButton
                    render={
                      <ShellLink
                        href={navigationHref(child.href, activeWorkspaceId)}
                        onNavigate={onNavigate}
                        aria-current={isActivePath(currentPath, child.href) ? 'page' : undefined}
                        title={msg(child.labelId, child.label)}
                      />
                    }
                    isActive={isActivePath(currentPath, child.href)}
                    className="h-9 translate-x-0 gap-3 pl-5 pr-2"
                  >
                    {ChildIcon ? <ChildIcon className="size-4" aria-hidden /> : null}
                    <span className="flex-1 truncate">{msg(child.labelId, child.label)}</span>
                  </SidebarMenuSubButton>
                </SidebarMenuSubItem>
              );
            })}
          </SidebarMenuSub>
        </CollapsibleContent>
      </Collapsible>
    </SidebarMenuItem>
  );
}

function ShellSidebarHeader({ currentPath, onNavigate }: { currentPath: string; onNavigate?: NavigateHandler }) {
  const { state, isMobile } = useSidebar();
  return (
    <AppSidebarHeader
      className="console-sidebar-header"
      data-console-compact={state === 'collapsed' && !isMobile ? 'true' : undefined}
    >
      <WorkspaceSwitcher currentPath={currentPath} onNavigate={onNavigate} />
    </AppSidebarHeader>
  );
}

function WorkspaceSwitcher({ currentPath, onNavigate }: { currentPath: string; onNavigate?: NavigateHandler }) {
  const confirmation = useScopeConfirmation();
  const organizations = useOrganizations();
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const { isMobile, state } = useSidebar();
  const collapsed = state === 'collapsed' && !isMobile;
  const {
    canManageWorkspaces,
    workspaces,
    activeWorkspace,
    activeWorkspaceId,
    selectWorkspace,
    isLoading,
    createWorkspace,
  } = useWorkspace();

  const handleSelect = async (workspace: Workspace) => {
    setOpen(false);
    if (workspace.id === activeWorkspaceId) return;
    if (organizations) {
      confirmation.request(() => selectWorkspace(workspace.id));
      return;
    }
    await navigateToMatchingWorkspacePath(currentPath, workspace.id, onNavigate);
    selectWorkspace(workspace.id);
  };

  const handleCreate = async (name: string, displayColor: string) => {
    const created = await createWorkspace(buildCreateWorkspaceInput(name, displayColor));
    if (!organizations) await navigateToMatchingWorkspacePath(currentPath, created.id, onNavigate);
  };

  return (
    <SidebarMenu>
      {confirmation.dialog}
      <SidebarMenuItem>
        <DropdownMenu open={open} onOpenChange={setOpen}>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                type="button"
                size="lg"
                tooltip={msg('workspace.switcher', 'Switch workspace')}
                className={clsx(
                  'text-sidebar-foreground data-[popup-open]:bg-sidebar-accent data-[popup-open]:text-sidebar-accent-foreground',
                  interactiveMotionClass,
                  collapsed ? 'justify-center' : 'justify-start',
                )}
                aria-label={localizedWorkspaceName(activeWorkspace.name, msg)}
                title={localizedWorkspaceName(activeWorkspace.name, msg)}
              />
            }
          >
            <span className="grid aspect-square size-8 shrink-0 place-items-center rounded-lg border-[0.5px] border-sidebar-border bg-sidebar-accent text-sidebar-accent-foreground">
              <navigationIcons.workspace
                className="size-4"
                style={{ color: workspaceColor(activeWorkspace) }}
                aria-hidden
              />
            </span>
            {collapsed ? null : (
              <>
                <span className="grid min-w-0 flex-1 text-left text-sm leading-tight">
                  <span className="truncate font-semibold">{localizedWorkspaceName(activeWorkspace.name, msg)}</span>
                  <span className="truncate text-xs text-sidebar-foreground/70">
                    {msg('settings.workspaces.workspace', 'Workspace')}
                  </span>
                </span>
                <navigationIcons.chevronRight
                  className="ml-auto size-4 shrink-0 rotate-90 text-sidebar-foreground/70"
                  aria-hidden
                />
              </>
            )}
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="start"
            side="bottom"
            sideOffset={4}
            className="console-navigation-menu subtle-scrollbar w-(--anchor-width) min-w-56 max-w-[calc(100vw-2rem)] p-1"
          >
            <DropdownMenuGroup>
              <DropdownMenuLabel className="text-xs text-muted-foreground">
                {msg('nav.workspaces', 'Workspaces')}
              </DropdownMenuLabel>
              <div className="subtle-scrollbar max-h-[240px] overflow-y-auto pr-0.5">
                {workspaces.map((workspace) => (
                  <DropdownMenuItem
                    key={workspace.id}
                    aria-current={workspace.id === activeWorkspaceId ? 'true' : undefined}
                    className="gap-2 p-2"
                    onClick={() => handleSelect(workspace)}
                  >
                    <span className="grid size-6 shrink-0 place-items-center rounded-md border-[0.5px] bg-background text-muted-foreground">
                      <navigationIcons.workspace
                        className="size-4"
                        style={{ color: workspaceColor(workspace) }}
                        aria-hidden
                      />
                    </span>
                    <span className="min-w-0 flex-1 truncate">{localizedWorkspaceName(workspace.name, msg)}</span>
                  </DropdownMenuItem>
                ))}
              </div>
            </DropdownMenuGroup>

            {canManageWorkspaces ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  className="gap-2 p-2"
                  onClick={() => {
                    setOpen(false);
                    confirmation.request(() => setCreateOpen(true));
                  }}
                >
                  <span className="grid size-6 shrink-0 place-items-center rounded-md border-[0.5px] bg-background">
                    <Plus className="size-4" aria-hidden />
                  </span>
                  <span className="min-w-0 flex-1 truncate font-medium text-muted-foreground">
                    {msg('workspace.create.title', 'Create workspace')}
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="gap-2 p-2"
                  render={<ShellLink href="/settings/workspaces" onNavigate={onNavigate} />}
                  onClick={() => setOpen(false)}
                >
                  <span className="grid size-6 shrink-0 place-items-center rounded-md border-[0.5px] bg-background">
                    <Settings className="size-4" aria-hidden />
                  </span>
                  <span className="min-w-0 flex-1 truncate font-medium text-muted-foreground">
                    {msg('workspace.manage.title', 'Manage workspaces')}
                  </span>
                </DropdownMenuItem>
              </>
            ) : null}

            {isLoading ? (
              <div className="px-3 pt-2 text-xs text-muted-foreground">
                {msg('workspace.loading', 'Loading workspaces...')}
              </div>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>

      <CreateWorkspaceDialog open={createOpen} onOpenChange={setCreateOpen} onCreate={handleCreate} />
    </SidebarMenu>
  );
}

function SidebarLink({
  collapsed,
  item,
  currentPath,
  onNavigate,
}: {
  collapsed?: boolean;
  item: NavLinkItem;
  currentPath: string;
  onNavigate?: NavigateHandler;
}) {
  const Icon = item.icon;
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  const href = navigationHref(item.href, activeWorkspaceId);

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        render={
          <ShellLink
            href={href}
            onNavigate={onNavigate}
            aria-current={isActivePath(currentPath, item.href) ? 'page' : undefined}
            title={msg(item.labelId, item.label)}
          />
        }
        isActive={isActivePath(currentPath, item.href)}
        tooltip={msg(item.labelId, item.label)}
        className={interactiveMotionClass}
        aria-label={collapsed ? msg(item.labelId, item.label) : undefined}
      >
        {Icon ? <Icon className="size-4" aria-hidden /> : null}
        {collapsed ? null : <span className="truncate">{msg(item.labelId, item.label)}</span>}
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function SidebarFooter({
  account,
  collapsed,
  onLogout,
  onNavigate,
  workspaceEntry,
}: {
  account?: AuthAccount | null;
  collapsed?: boolean;
  onLogout: () => Promise<void> | void;
  onNavigate?: NavigateHandler;
  workspaceEntry?: ReactNode;
}) {
  return (
    <AppSidebarFooter className="mt-auto px-2 py-2">
      <SidebarMenu data-console-footer-links>{workspaceEntry}</SidebarMenu>
      <PlatformReturnButton collapsed={collapsed} />
      <AccountMenu account={account} collapsed={collapsed} onLogout={onLogout} onNavigate={onNavigate} />
    </AppSidebarFooter>
  );
}

export function AccountMenu({
  account,
  collapsed,
  onLogout,
  onNavigate,
}: {
  account?: AuthAccount | null;
  collapsed?: boolean;
  onLogout: () => Promise<void> | void;
  onNavigate?: NavigateHandler;
}) {
  const { isMobile } = useSidebar();
  const [open, setOpen] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const identity = getIdentity(account);
  const { orgUuid } = useWorkspace();
  const organizationAccess = useContext(AuthContext)?.organizationAccess;
  const organizationName = currentOrganizationName(account, organizationAccess, orgUuid);
  const { locale, setLocale, supportedLocales: locales } = useLocale();
  const { msg } = useI18n();

  const handleMenuNavigation = async (href: string) => {
    setOpen(false);
    if (onNavigate) {
      await onNavigate(href);
      return;
    }
    if (typeof window !== 'undefined') {
      window.location.assign(href);
    }
  };

  const handleLogout = async () => {
    setLoggingOut(true);
    try {
      await onLogout();
      setOpen(false);
    } finally {
      setLoggingOut(false);
    }
  };

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu open={open} onOpenChange={setOpen}>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                className={clsx(
                  interactiveMotionClass,
                  'data-[popup-open]:bg-sidebar-accent data-[popup-open]:text-sidebar-accent-foreground',
                  collapsed ? 'justify-center' : '',
                )}
                aria-label={collapsed ? identity.name : undefined}
              />
            }
          >
            <span className="grid size-8 place-items-center rounded-md border-[0.5px] border-sidebar-border bg-sidebar-accent/40 text-sidebar-foreground">
              <Building2 className="size-4" aria-hidden />
            </span>
            {collapsed ? null : (
              <>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium text-sidebar-foreground">{identity.name}</span>
                  <span className="flex min-w-0 items-center gap-2 text-xs text-sidebar-foreground/70">
                    <span className="min-w-0 flex-1 truncate" title={organizationName}>
                      {organizationName}
                    </span>
                  </span>
                </span>
                <ChevronDown className="size-4 text-sidebar-foreground/70" aria-hidden />
              </>
            )}
          </DropdownMenuTrigger>

          <DropdownMenuContent
            align="end"
            side={isMobile ? 'bottom' : 'right'}
            sideOffset={6}
            className="console-navigation-menu subtle-scrollbar w-[288px] max-w-[calc(100vw-2rem)] p-1"
          >
            <DropdownMenuGroup>
              <DropdownMenuLabel className="truncate px-3 py-2 text-xs">{identity.email}</DropdownMenuLabel>
            </DropdownMenuGroup>

            <OrganizationMenu />

            <DropdownMenuSeparator />

            <PlatformAdminMenuItem onNavigate={handleMenuNavigation} />

            <AppearanceMenuRow />

            <DropdownMenuSub>
              <DropdownMenuSubTrigger openOnHover className="h-8 gap-3 px-3">
                <Globe2 className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                <span className="min-w-0 flex-1 truncate">{msg('language.label', 'Language')}</span>
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="console-navigation-menu subtle-scrollbar min-w-[228px] max-w-[calc(100vw-2rem)]">
                <DropdownMenuRadioGroup value={locale}>
                  {locales.map((option) => (
                    <DropdownMenuRadioItem
                      key={option}
                      value={option}
                      className="h-8 gap-2 px-3"
                      onClick={() => {
                        setLocale(option);
                        setOpen(false);
                      }}
                    >
                      <span className="min-w-0 flex-1 truncate">{localeLabels[option]}</span>
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            <DropdownMenuSeparator />

            <DropdownMenuItem
              closeOnClick={false}
              disabled={loggingOut}
              className="h-8 gap-3 px-3 disabled:text-muted-foreground"
              onClick={() => void handleLogout()}
            >
              <LogOut className="size-4 shrink-0 text-muted-foreground" aria-hidden />
              <span className="min-w-0 flex-1 truncate">
                {loggingOut ? msg('account.loggingOut', 'Logging out...') : msg('account.logout', 'Log out')}
              </span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

async function navigateToMatchingWorkspacePath(currentPath: string, workspaceId: string, onNavigate?: NavigateHandler) {
  const nextPath = workspaceSwitchPath(currentPath, workspaceId);

  if (nextPath === currentPath) {
    return;
  }

  if (onNavigate) {
    await onNavigate(nextPath);
    return;
  }

  if (typeof window !== 'undefined') {
    window.location.assign(nextPath);
  }
}

function getIdentity(account?: AuthAccount | null) {
  const email = account?.email_address ?? 'test@openmanagedagent.local';
  const emailName = email.split('@')[0] || 'test';
  return {
    email,
    name: account?.display_name ?? account?.full_name ?? emailName,
  };
}
