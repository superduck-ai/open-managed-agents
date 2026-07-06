import {
  ArrowLeft,
  BookOpen,
  Box,
  Building2,
  Check,
  ChevronDown,
  ChevronRight,
  CircleHelp,
  Gauge,
  Globe2,
  KeyRound,
  ListTree,
  LockKeyhole,
  LogOut,
  MessageSquare,
  Palette,
  Plus,
  Scale,
  Settings,
  Shield,
  UsersRound,
  WalletCards,
} from 'lucide-react';
import { Outlet, useLocation, useNavigate } from '@tanstack/react-router';
import {
  useCallback,
  useEffect,
  forwardRef,
  useState,
  type AnchorHTMLAttributes,
  type MouseEvent,
  type ReactNode
} from 'react';
import clsx from 'clsx';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import {
  Sidebar as AppSidebar,
  SidebarContent as AppSidebarContent,
  SidebarFooter as AppSidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader as AppSidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  useSidebar
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
  DropdownMenuTrigger
} from '@/shared/ui/dropdown-menu';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';
import { useAuth } from '../../shared/auth/context';
import type { AuthAccount } from '../../shared/auth/api';
import { useWorkspace } from '../../shared/workspaces/context';
import type { Workspace } from '../../shared/workspaces/api';
import { CreateWorkspaceDialog } from '../../shared/workspaces/CreateWorkspaceDialog';
import {
  buildCreateWorkspaceInput,
  workspaceApiKeysPath,
  workspaceColor,
  workspaceIdFromPath,
  workspaceWebhooksPath
} from '../../shared/workspaces/presentation';
import { useI18n, useLocale } from '../../shared/i18n';
import { consoleNavigation, settingsNavigation, type NavLinkItem } from './navigation';

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
const interactiveMotionClass =
  'transition-colors duration-200 ease-snappy-out motion-reduce:transition-none';

const ShellLink = forwardRef<HTMLAnchorElement, ShellLinkProps>(function ShellLink(
  { href, onNavigate, onClick, target, children, ...props },
  ref
) {
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (
      event.defaultPrevented ||
      !onNavigate ||
      target ||
      event.button !== 0 ||
      event.metaKey ||
      event.altKey ||
      event.ctrlKey ||
      event.shiftKey
    ) {
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

  const handleNavigate = useCallback(
    async (href: string) => {
      await navigate({ href });
    },
    [navigate]
  );

  const handleLogout = async () => {
    await logout();
    await navigate({ to: '/login', search: { returnTo: '/' } });
  };

  return (
    <ConsoleShell
      account={account}
      currentPath={location.pathname}
      onLogout={handleLogout}
      onNavigate={handleNavigate}
    >
      <Outlet />
    </ConsoleShell>
  );
}

export function ConsoleShell({ account, currentPath = '/', children, onLogout, onNavigate }: ConsoleShellProps) {
  const isWide = isWideConsolePath(currentPath);
  const { msg } = useI18n();

  return (
    <SidebarProvider defaultOpen>
      <ConsoleSidebar
        account={account}
        currentPath={currentPath}
        onLogout={onLogout}
        onNavigate={onNavigate}
      />
      <SidebarInset className="min-h-screen text-foreground">
        <ShellMobileBar
          title={msg('app.productName', 'Open Managed Agents')}
          toggleLabel={msg('common.expand', 'Expand')}
          activeToggleLabel={msg('common.collapse', 'Collapse')}
          href="/dashboard"
          onNavigate={onNavigate}
        />
        <div className={clsx(isWide ? 'px-6 py-6 lg:px-8' : 'mx-auto max-w-[928px] px-6 py-12 lg:px-0')}>
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

type SettingsShellProps = ConsoleShellProps;

export function SettingsShell({
  account,
  currentPath = '/settings/organization',
  children,
  onLogout,
  onNavigate
}: SettingsShellProps) {
  const { msg } = useI18n();

  return (
    <SidebarProvider defaultOpen>
      <SettingsSidebar
        account={account}
        currentPath={currentPath}
        onLogout={onLogout}
        onNavigate={onNavigate}
      />
      <SidebarInset className="min-h-screen text-foreground">
        <ShellMobileBar
          title={msg('account.organizationSettings', 'Organization settings')}
          toggleLabel={msg('common.expand', 'Expand')}
          activeToggleLabel={msg('common.collapse', 'Collapse')}
          href="/settings/organization"
          onNavigate={onNavigate}
        />
        <div className="px-6 py-6 lg:px-8">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function ConsoleSidebar({
  account,
  currentPath = '/',
  onLogout,
  onNavigate
}: Omit<ConsoleShellProps, 'children'>) {
  const { msg } = useI18n();
  const { activeWorkspaceId, selectWorkspace, workspaces } = useWorkspace();
  const { setOpen, state } = useSidebar();
  const collapsed = state === 'collapsed';
  const [expanded, setExpanded] = useState<Record<string, boolean>>({
    Build: true,
    'Managed Agents': true,
    Analytics: true,
    'Claude Code': true,
    Manage: true
  });
  const routeWorkspaceId = workspaceIdFromPath(currentPath);

  useEffect(() => {
    if (!routeWorkspaceId || routeWorkspaceId === activeWorkspaceId) {
      return;
    }
    if (workspaces.some((workspace) => workspace.id === routeWorkspaceId)) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace, workspaces]);

  return (
    <AppSidebar
      collapsible="icon"
      className="border-r border-sidebar-border"
      data-sidebar-state={collapsed ? 'collapsed' : 'expanded'}
    >
      <ShellSidebarHeader collapsed={collapsed} onNavigate={onNavigate} />
      <div className="shrink-0 px-2 py-2">
        <WorkspaceSwitcher currentPath={currentPath} onNavigate={onNavigate} />
      </div>
      <SidebarSeparator />
      <AppSidebarContent
        className="sidebar-scroll-area px-2 py-2"
        data-sidebar-scroll-area="true"
      >
        <SidebarGroup className="p-0">
          <SidebarGroupContent>
            <nav aria-label={msg('nav.consoleNavigation', 'Console navigation')}>
              <SidebarMenu>
                {consoleNavigation.map((item) => {
                  if (item.type === 'link') {
                    return (
                      <SidebarLink
                        key={item.href}
                        collapsed={collapsed}
                        item={item}
                        currentPath={currentPath}
                        onNavigate={onNavigate}
                      />
                    );
                  }

                  const Icon = item.icon;
                  const isOpen = expanded[item.label] ?? true;
                  const groupActive = item.children.some((child) => isActivePath(currentPath, child.href));

                  return (
                    <SidebarMenuItem key={item.label} className="pt-1">
                      <SidebarMenuButton
                        type="button"
                      isActive={groupActive}
                      tooltip={msg(item.labelId, item.label)}
                      className={interactiveMotionClass}
                      aria-label={collapsed ? msg(item.labelId, item.label) : undefined}
                      aria-expanded={collapsed ? false : isOpen}
                      onClick={() => {
                          if (collapsed) {
                            setOpen(true);
                            setExpanded((value) => ({ ...value, [item.label]: true }));
                            return;
                          }
                          setExpanded((value) => ({ ...value, [item.label]: !isOpen }));
                        }}
                      >
                        <Icon className="size-4" aria-hidden />
                        {collapsed ? null : (
                          <>
                            <span className="flex-1 truncate">{msg(item.labelId, item.label)}</span>
                            {isOpen ? (
                              <ChevronDown className="size-3.5 text-muted-foreground" aria-hidden />
                            ) : (
                              <ChevronRight className="size-3.5 text-muted-foreground" aria-hidden />
                            )}
                          </>
                        )}
                      </SidebarMenuButton>
                      {!collapsed && isOpen ? (
                        <SidebarMenuSub>
                          {item.children.map((child) => (
                            <SidebarMenuSubItem key={child.href}>
                              <SidebarMenuSubButton
                                render={
                                  <ShellLink
                                    href={navigationHref(child.href, activeWorkspaceId)}
                                    onNavigate={onNavigate}
                                  />
                                }
                                isActive={isActivePath(currentPath, child.href)}
                                className={interactiveMotionClass}
                              >
                                <span className="flex-1 truncate">{msg(child.labelId, child.label)}</span>
                                {child.badge ? (
                                  <Badge>
                                    {child.badgeId ? msg(child.badgeId, child.badge) : child.badge}
                                  </Badge>
                                ) : null}
                              </SidebarMenuSubButton>
                            </SidebarMenuSubItem>
                          ))}
                        </SidebarMenuSub>
                      ) : null}
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </nav>
          </SidebarGroupContent>
        </SidebarGroup>
      </AppSidebarContent>
      <SidebarSeparator />
      <SidebarFooter account={account} collapsed={collapsed} onLogout={onLogout} onNavigate={onNavigate} />
      <SidebarRail />
    </AppSidebar>
  );
}

function SettingsSidebar({
  account,
  currentPath = '/settings/organization',
  onLogout,
  onNavigate
}: Omit<ConsoleShellProps, 'children'>) {
  const { msg } = useI18n();
  const { state } = useSidebar();
  const collapsed = state === 'collapsed';

  return (
    <AppSidebar
      collapsible="icon"
      className="border-r border-sidebar-border"
      data-sidebar-state={collapsed ? 'collapsed' : 'expanded'}
    >
      <ShellSidebarHeader collapsed={collapsed} onNavigate={onNavigate} />
      <AppSidebarContent className="sidebar-scroll-area px-2 py-2" data-sidebar-scroll-area="true">
        <SidebarGroup className="p-0">
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton
                  render={<ShellLink href="/dashboard" onNavigate={onNavigate} />}
                  tooltip={msg('nav.backToApp', 'Back to app')}
                  className={interactiveMotionClass}
                  aria-label={collapsed ? msg('nav.backToApp', 'Back to app') : undefined}
                >
                  <ArrowLeft className="size-4" aria-hidden />
                  {collapsed ? null : <span className="truncate">{msg('nav.backToApp', 'Back to app')}</span>}
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup className="mt-2 p-0">
          <SidebarGroupLabel className="gap-2 px-2 text-sidebar-foreground">
            <Settings className="size-4" aria-hidden />
            {msg('account.organizationSettings', 'Organization settings')}
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <nav aria-label={msg('nav.settingsNavigation', 'Settings navigation')}>
              <SidebarMenu>
                {settingsNavigation.map((item) => (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                    render={<ShellLink href={item.href} onNavigate={onNavigate} />}
                    isActive={isActivePath(currentPath, item.href)}
                    tooltip={msg(item.labelId, item.label)}
                    className={interactiveMotionClass}
                    aria-label={collapsed ? msg(item.labelId, item.label) : undefined}
                  >
                      <SettingsNavigationIcon href={item.href} />
                      {collapsed ? null : <span className="truncate">{msg(item.labelId, item.label)}</span>}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </nav>
          </SidebarGroupContent>
        </SidebarGroup>
      </AppSidebarContent>
      <SidebarSeparator />
      <SidebarFooter account={account} collapsed={collapsed} onLogout={onLogout} onNavigate={onNavigate} />
      <SidebarRail />
    </AppSidebar>
  );
}

function ShellMobileBar({
  title,
  toggleLabel,
  activeToggleLabel,
  href,
  onNavigate
}: {
  title: string;
  toggleLabel: string;
  activeToggleLabel: string;
  href: string;
  onNavigate?: NavigateHandler;
}) {
  const { isMobile, openMobile, toggleSidebar } = useSidebar();

  if (!isMobile) {
    return null;
  }

  return (
    <div className="sticky top-0 z-20 flex items-center gap-3 border-b border-border bg-background/95 px-4 py-3 backdrop-blur-sm md:hidden">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="shrink-0"
        aria-label={openMobile ? activeToggleLabel : toggleLabel}
        onClick={toggleSidebar}
      >
        <ListTree className="size-4" aria-hidden />
      </Button>
      <ShellLink
        href={href}
        onNavigate={onNavigate}
        className="min-w-0 truncate text-sm font-semibold tracking-[-0.01em] text-foreground"
      >
        {title}
      </ShellLink>
    </div>
  );
}

function ShellSidebarHeader({
  collapsed,
  onNavigate
}: {
  collapsed?: boolean;
  onNavigate?: NavigateHandler;
}) {
  const { msg } = useI18n();
  const { toggleSidebar } = useSidebar();

  return (
    <AppSidebarHeader className="gap-0 border-b px-2 py-2">
      <div className={clsx('flex min-h-10 items-center gap-2', collapsed ? 'justify-center' : 'justify-between')}>
        {collapsed ? null : (
          <ShellLink
            href="/dashboard"
            onNavigate={onNavigate}
            className="min-w-0 truncate px-2 text-base font-semibold tracking-[-0.01em] text-sidebar-foreground"
          >
            {msg('app.productName', 'Open Managed Agents')}
          </ShellLink>
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          aria-label={collapsed ? msg('common.expand', 'Expand') : msg('common.collapse', 'Collapse')}
          aria-pressed={collapsed}
          onClick={toggleSidebar}
        >
          <ListTree className="size-4" aria-hidden />
        </Button>
      </div>
    </AppSidebarHeader>
  );
}

function WorkspaceSwitcher({
  currentPath,
  onNavigate
}: {
  currentPath: string;
  onNavigate?: NavigateHandler;
}) {
  const { msg } = useI18n();
  const { state } = useSidebar();
  const collapsed = state === 'collapsed';
  const [open, setOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const { workspaces, activeWorkspace, activeWorkspaceId, selectWorkspace, isLoading, createWorkspace } =
    useWorkspace();

  const handleSelect = (workspace: Workspace) => {
    selectWorkspace(workspace.id);
    setOpen(false);
    void navigateToMatchingWorkspacePath(currentPath, workspace.id, onNavigate);
  };

  const handleCreate = async (name: string, displayColor: string) => {
    const created = await createWorkspace(buildCreateWorkspaceInput(name, displayColor));
    await navigateToMatchingWorkspacePath(currentPath, created.id, onNavigate);
  };

  return (
    <SidebarMenu>
      <SidebarMenuItem>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <SidebarMenuButton
              type="button"
              size="lg"
              role="combobox"
              aria-label={activeWorkspace.name}
              aria-expanded={open}
              aria-controls="workspace-switcher-listbox"
              className={clsx(
                'h-10 text-sidebar-foreground aria-expanded:bg-sidebar-accent aria-expanded:text-sidebar-accent-foreground',
                interactiveMotionClass,
                collapsed ? 'justify-center' : 'justify-between'
              )}
            />
          }
        >
          <span className="flex min-w-0 items-center gap-2">
            <Box className="size-4 shrink-0" style={{ color: workspaceColor(activeWorkspace) }} aria-hidden />
            {collapsed ? null : <span className="truncate">{activeWorkspace.name}</span>}
          </span>
          {collapsed ? null : <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />}
        </PopoverTrigger>
        <PopoverContent
          id="workspace-switcher-listbox"
          role="listbox"
          align="start"
          side={collapsed ? 'right' : 'bottom'}
          sideOffset={8}
          className="w-[calc(var(--anchor-width)+2rem)] min-w-[264px] max-w-[calc(100vw-2rem)] gap-0 p-2"
        >
          <div className="subtle-scrollbar max-h-[264px] overflow-y-auto pr-0.5">
            <Button
              type="button"
              role="option"
              aria-disabled="true"
              disabled
              variant="ghost"
              className="h-auto w-full items-start justify-start gap-3 rounded-md px-3 py-2 text-left text-sm font-normal text-muted-foreground/70"
            >
              <Box className="mt-0.5 size-4 shrink-0" aria-hidden />
              <span>
                <span className="block text-muted-foreground">{msg('workspace.all', 'All workspaces')}</span>
                <span className="mt-0.5 block text-xs text-muted-foreground/70">
                  {msg('workspace.onlyCostLogs', 'Only available on Cost and Logs')}
                </span>
              </span>
            </Button>

            {workspaces.map((workspace) => {
              const selected = workspace.id === activeWorkspaceId;
              return (
                <Button
                  key={workspace.id}
                  type="button"
                  role="option"
                  aria-selected={selected}
                  variant="ghost"
                  className="mt-1 h-9 w-full justify-start gap-3 rounded-md px-3 text-left text-sm font-normal text-foreground hover:bg-accent"
                  onClick={() => handleSelect(workspace)}
                >
                  <Box className="size-4 shrink-0" style={{ color: workspaceColor(workspace) }} aria-hidden />
                  <span className="min-w-0 flex-1 truncate">{workspace.name}</span>
                  {selected ? <Check className="size-4 text-primary" aria-hidden /> : null}
                </Button>
              );
            })}
          </div>

          <div className="mx-3 my-2 h-px bg-border" />

          <Button
            type="button"
            variant="ghost"
            className="h-9 w-full justify-start gap-3 rounded-md px-3 text-left text-sm font-normal text-foreground hover:bg-accent"
            onClick={() => {
              setOpen(false);
              setCreateOpen(true);
            }}
          >
            <Plus className="size-4" aria-hidden />
            {msg('workspace.create.title', 'Create workspace')}
          </Button>

            {isLoading ? (
              <div className="px-3 pt-2 text-xs text-muted-foreground">
                {msg('workspace.loading', 'Loading workspaces...')}
              </div>
            ) : null}
          </PopoverContent>
      </Popover>
      </SidebarMenuItem>

      <CreateWorkspaceDialog open={createOpen} onOpenChange={setCreateOpen} onCreate={handleCreate} />
    </SidebarMenu>
  );
}

function SidebarLink({
  collapsed,
  item,
  currentPath,
  onNavigate
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
        render={<ShellLink href={href} onNavigate={onNavigate} />}
        isActive={isActivePath(currentPath, item.href)}
        tooltip={msg(item.labelId, item.label)}
        className={interactiveMotionClass}
        aria-label={collapsed ? msg(item.labelId, item.label) : undefined}
      >
        {Icon ? <Icon className="size-4" aria-hidden /> : null}
        {collapsed ? null : <span className="truncate">{msg(item.labelId, item.label)}</span>}
      </SidebarMenuButton>
      {item.badge ? (
        <SidebarMenuBadge>{item.badgeId ? msg(item.badgeId, item.badge) : item.badge}</SidebarMenuBadge>
      ) : null}
    </SidebarMenuItem>
  );
}

function SidebarFooter({
  account,
  collapsed,
  onLogout,
  onNavigate
}: {
  account?: AuthAccount | null;
  collapsed?: boolean;
  onLogout: () => Promise<void> | void;
  onNavigate?: NavigateHandler;
}) {
  const { msg } = useI18n();

  return (
    <AppSidebarFooter className="mt-auto px-2 py-2">
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton
            render={<a href="https://docs.anthropic.com/" target="_blank" rel="noreferrer" />}
            tooltip={msg('nav.documentation', 'Documentation')}
            className={interactiveMotionClass}
            aria-label={collapsed ? msg('nav.documentation', 'Documentation') : undefined}
          >
            <BookOpen className="size-4" aria-hidden />
            {collapsed ? null : <span className="truncate">{msg('nav.documentation', 'Documentation')}</span>}
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
      <AccountMenu account={account} collapsed={collapsed} onLogout={onLogout} onNavigate={onNavigate} />
    </AppSidebarFooter>
  );
}

function SettingsNavigationIcon({ href }: { href: string }) {
  if (href === '/settings/appearance') {
    return <Palette className="size-4" aria-hidden />;
  }
  if (href === '/settings/organization') {
    return <Building2 className="size-4" aria-hidden />;
  }
  if (href === '/settings/members') {
    return <UsersRound className="size-4" aria-hidden />;
  }
  if (href === '/settings/workspaces') {
    return <Box className="size-4" aria-hidden />;
  }
  if (href === '/settings/billing') {
    return <WalletCards className="size-4" aria-hidden />;
  }
  if (href === '/settings/limits') {
    return <Gauge className="size-4" aria-hidden />;
  }
  if (href === '/settings/api-keys' || href === '/settings/admin-keys') {
    return <KeyRound className="size-4" aria-hidden />;
  }
  if (href === '/settings/service-accounts') {
    return <LockKeyhole className="size-4" aria-hidden />;
  }
  if (
    href === '/settings/workload-identity' ||
    href === '/settings/privacy-controls' ||
    href === '/settings/identity-and-access'
  ) {
    return <Shield className="size-4" aria-hidden />;
  }
  return <Settings className="size-4" aria-hidden />;
}

export function AccountMenu({
  account,
  collapsed,
  onLogout,
  onNavigate
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
  const { activeWorkspace } = useWorkspace();
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
                  collapsed ? 'justify-center' : ''
                )}
                aria-label={collapsed ? identity.name : undefined}
              />
            }
          >
            <span className="grid size-8 place-items-center rounded-md border border-sidebar-border bg-sidebar-accent/40 text-sidebar-foreground">
              <Building2 className="size-4" aria-hidden />
            </span>
            {collapsed ? null : (
              <>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium text-sidebar-foreground">{identity.name}</span>
                  <span className="block truncate text-xs text-sidebar-foreground/70">
                    {msg('account.subtitle', 'Admin · {workspaceName}', { workspaceName: activeWorkspace.name })}
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
            className="w-[288px] overflow-visible p-1"
          >
        <DropdownMenuGroup>
          <DropdownMenuLabel className="truncate px-3 py-2 text-xs">{identity.email}</DropdownMenuLabel>
        </DropdownMenuGroup>

        <DropdownMenuRadioGroup value={activeWorkspace.id}>
          <DropdownMenuRadioItem
            value={activeWorkspace.id}
            closeOnClick={false}
            className="h-12 items-start gap-3 px-3 py-2.5 text-foreground"
          >
            <Building2 className="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{activeWorkspace.name}</span>
              <span className="block text-xs text-muted-foreground">{msg('account.apiPlan', 'API plan')}</span>
            </span>
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>

        <DropdownMenuSeparator />

        <DropdownMenuItem className="h-8 gap-3 px-3" onClick={() => void handleMenuNavigation('/settings/organization')}>
          <Settings className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <span className="min-w-0 flex-1 truncate">{msg('account.organizationSettings', 'Organization settings')}</span>
        </DropdownMenuItem>

        <DropdownMenuItem className="h-8 gap-3 px-3" onClick={() => setOpen(false)}>
          <MessageSquare className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <span className="min-w-0 flex-1 truncate">{msg('account.feedback', 'Feedback')}</span>
        </DropdownMenuItem>

        <DropdownMenuItem
          className="h-8 gap-3 px-3"
          render={<a href="https://support.anthropic.com/" target="_blank" rel="noreferrer" />}
        >
          <CircleHelp className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <span className="min-w-0 flex-1 truncate">{msg('account.getHelp', 'Get help')}</span>
        </DropdownMenuItem>

        <DropdownMenuSub>
          <DropdownMenuSubTrigger openOnHover className="h-8 gap-3 px-3">
            <Globe2 className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <span className="min-w-0 flex-1 truncate">{msg('language.label', 'Language')}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="min-w-[228px]">
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
                  <span className="min-w-0 flex-1 truncate">
                    {msg(
                      option === 'zh-CN' ? 'language.simplifiedChinese' : 'language.english',
                      option === 'zh-CN' ? 'Simplified Chinese' : 'English'
                    )}
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        <DropdownMenuSub>
          <DropdownMenuSubTrigger openOnHover className="h-8 gap-3 px-3">
            <Scale className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <span className="min-w-0 flex-1 truncate">{msg('account.legalCenter', 'Legal center')}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="min-w-[190px]">
            {legalMenuItems.map((item) => (
              <DropdownMenuItem
                key={item.labelId}
                className="h-8 px-3 text-foreground"
                render={<a href={item.href} target="_blank" rel="noreferrer" />}
              >
                <span className="truncate">{msg(item.labelId, item.label)}</span>
              </DropdownMenuItem>
            ))}
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

const legalMenuItems = [
  {
    label: 'Commercial Terms',
    labelId: 'account.commercialTerms',
    href: 'https://www.anthropic.com/legal/commercial-terms'
  },
  { label: 'Usage Policy', labelId: 'account.usagePolicy', href: 'https://www.anthropic.com/legal/aup' },
  { label: 'Privacy Policy', labelId: 'account.privacyPolicy', href: 'https://www.anthropic.com/legal/privacy' },
  {
    label: 'Data retention',
    labelId: 'account.dataRetention',
    href: 'https://support.anthropic.com/en/articles/7996866-how-long-do-you-store-personal-data'
  },
  { label: 'Your Privacy Choices', labelId: 'account.privacyChoices', href: 'https://www.anthropic.com/privacy' }
];

const managedAgentPathByHref: Record<string, string> = {
  '/quickstart': 'agent-quickstart',
  '/agents': 'agents',
  '/sessions': 'sessions',
  '/deployments': 'deployments',
  '/environments': 'environments',
  '/credential-vaults': 'vaults',
  '/memory-stores': 'memory-stores',
  '/dreams': 'dreams'
};

const workspaceBuildPathByHref: Record<string, string> = {
  '/playground': 'playground',
  '/files': 'files',
  '/skills': 'skills',
  '/batches': 'batches'
};

function navigationHref(href: string, workspaceId: string) {
  if (href === '/api-keys') {
    return workspaceApiKeysPath(workspaceId);
  }
  if (href === '/webhooks') {
    return workspaceWebhooksPath(workspaceId);
  }

  const buildPath = workspaceBuildPathByHref[href];
  if (buildPath) {
    return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/${buildPath}`;
  }

  const managedPath = managedAgentPathByHref[href];
  if (managedPath) {
    return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/${managedPath}`;
  }

  return href;
}

async function navigateToMatchingWorkspacePath(
  currentPath: string,
  workspaceId: string,
  onNavigate?: NavigateHandler
) {
  const encodedWorkspaceId = encodeURIComponent(workspaceId || 'default');
  let nextPath: string | undefined;

  if (currentPath === '/api-keys') {
    nextPath = workspaceApiKeysPath(workspaceId);
  } else if (currentPath === '/webhooks') {
    nextPath = workspaceWebhooksPath(workspaceId);
  } else {
    for (const [href, buildPath] of Object.entries(workspaceBuildPathByHref)) {
      if (currentPath === href) {
        nextPath = `/workspaces/${encodedWorkspaceId}/${buildPath}`;
        break;
      }
    }

    for (const [href, managedPath] of Object.entries(managedAgentPathByHref)) {
      if (!nextPath && currentPath === href) {
        nextPath = `/workspaces/${encodedWorkspaceId}/${managedPath}`;
        break;
      }
    }
  }

  nextPath ??= currentPath
    .replace(/^\/settings\/workspaces\/[^/]+\/keys/, workspaceApiKeysPath(workspaceId))
    .replace(/^\/settings\/workspaces\/[^/]+\/webhooks/, workspaceWebhooksPath(workspaceId))
    .replace(
      /^\/workspaces\/[^/]+\/(playground|files|skills|batches)/,
      `/workspaces/${encodedWorkspaceId}/$1`
    )
    .replace(
      /^\/workspaces\/[^/]+\/(agent-quickstart|agents|sessions|deployments|environments|vaults|memory-stores|dreams)/,
      `/workspaces/${encodedWorkspaceId}/$1`
    );

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
    name: account?.display_name ?? account?.full_name ?? emailName
  };
}

function isActivePath(currentPath: string, href: string) {
  if (href === '/dashboard') {
    return currentPath === '/' || currentPath === '/dashboard';
  }
  if (href === '/api-keys') {
    return currentPath === '/api-keys' || /^\/settings\/workspaces\/[^/]+\/keys/.test(currentPath);
  }
  if (href === '/webhooks') {
    return currentPath === '/webhooks' || /^\/settings\/workspaces\/[^/]+\/webhooks/.test(currentPath);
  }
  if (href === '/usage') {
    return currentPath === '/usage';
  }
  if (href === '/usage/cache') {
    return currentPath === '/usage/cache' || currentPath === '/caching';
  }
  if (href === '/usage/limits') {
    return currentPath === '/usage/limits' || currentPath === '/rate-limits';
  }
  if (href === '/cost') {
    return currentPath === '/cost' || /^\/workspaces\/[^/]+\/cost(\/|$)/.test(currentPath);
  }
  if (href === '/logs') {
    return currentPath === '/logs' || /^\/workspaces\/[^/]+\/logs(\/|$)/.test(currentPath);
  }
  const buildPath = workspaceBuildPathByHref[href];
  if (buildPath) {
    return currentPath === href || currentPath === `/workspaces/default/${buildPath}` || new RegExp(`^/workspaces/[^/]+/${buildPath}(/|$)`).test(currentPath);
  }
  const managedPath = managedAgentPathByHref[href];
  if (managedPath) {
    return currentPath === href || currentPath === `/workspaces/default/${managedPath}` || new RegExp(`^/workspaces/[^/]+/${managedPath}(/|$)`).test(currentPath);
  }
  return currentPath === href || currentPath.startsWith(`${href}/`);
}

function isWideConsolePath(currentPath: string) {
  return (
    currentPath === '/api-keys' ||
    /^\/settings\/workspaces\/[^/]+\/keys/.test(currentPath) ||
    currentPath === '/webhooks' ||
    /^\/settings\/workspaces\/[^/]+\/webhooks/.test(currentPath) ||
    isBuildPath(currentPath) ||
    isAnalyticsPath(currentPath) ||
    isManagedAgentsPath(currentPath)
  );
}

function isBuildPath(currentPath: string) {
  return (
    ['/workbench', '/playground', '/files', '/skills', '/batches'].includes(currentPath) ||
    currentPath.startsWith('/workbench/') ||
    /^\/workspaces\/[^/]+\/(?:playground|files|skills|batches)(\/|$)/.test(currentPath)
  );
}

function isAnalyticsPath(currentPath: string) {
  return (
    ['/usage', '/usage/cache', '/usage/limits', '/caching', '/rate-limits', '/cost', '/logs'].includes(currentPath) ||
    /^\/workspaces\/[^/]+\/(?:cost|logs)(\/|$)/.test(currentPath)
  );
}

function isManagedAgentsPath(currentPath: string) {
  return (
    ['/quickstart', '/agents', '/sessions', '/deployments', '/environments', '/credential-vaults', '/memory-stores', '/dreams'].includes(currentPath) ||
    /^\/workspaces\/[^/]+\/(agent-quickstart|agents|sessions|deployments|environments|vaults|memory-stores|dreams)(\/|$)/.test(
      currentPath
    )
  );
}
