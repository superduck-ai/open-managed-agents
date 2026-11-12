import { useContext, useState, type ReactNode } from 'react';
import { ArrowLeft } from 'lucide-react';
import type { AuthAccount } from '../../shared/auth/api';
import { AuthContext } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '../../shared/ui/accordion';
import {
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuSub,
  SidebarMenuSubItem,
  SidebarMenuSubButton,
  useSidebar,
} from '../../shared/ui/sidebar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { AccessDenied } from '../../shared/ui/access-denied';
import { isActivePath, isPlainNavigationClick, navigationHref } from '../../shared/navigation/navigation-paths';
import {
  canAccessSettings,
  currentOrganizationName,
  settingsItem,
  settingScope,
  visibleSettingsGroups,
} from '../../shared/navigation/settings-navigation';
import type { IconComponent, NavGroupItem } from '../../shared/navigation/navigation';

type NavigationProps = { currentPath: string; onNavigate?: (href: string) => void | Promise<void> };

export function CompactNavigationGroup({
  label,
  icon: Icon,
  items,
  currentPath,
  onNavigate,
}: NavigationProps & { label: string; icon: IconComponent; items: NavGroupItem['children'] }) {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <SidebarMenuButton
              aria-label={label}
              tooltip={label}
              isActive={items.some((item) => isActivePath(currentPath, item.href))}
            />
          }
        >
          <Icon className="size-4" aria-hidden />
        </DropdownMenuTrigger>
        <DropdownMenuContent side="right" align="start" className="console-navigation-menu min-w-48">
          {items.map((item) => {
            const ItemIcon = item.icon;
            return (
              <DropdownMenuItem
                key={item.href}
                render={<a href={navigationHref(item.href, activeWorkspaceId)} />}
                onClick={(event) => {
                  if (onNavigate && isPlainNavigationClick(event)) {
                    event.preventDefault();
                    void onNavigate(navigationHref(item.href, activeWorkspaceId));
                  }
                }}
                aria-current={isActivePath(currentPath, item.href) ? 'page' : undefined}
              >
                {ItemIcon && <ItemIcon className="size-4" aria-hidden />}
                {msg(item.labelId, item.label)}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  );
}

export function SettingsNavigation({
  account,
  currentPath,
  onNavigate,
  returnHref,
}: NavigationProps & {
  account?: AuthAccount | null;
  returnHref: string;
}) {
  const { msg } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const { state, isMobile } = useSidebar();
  const compact = state === 'collapsed' && !isMobile;
  const groups = visibleSettingsGroups(account, orgUuid);
  const selected = settingsItem(currentPath);
  const selectedGroupId = selected?.group.id;
  const [expanded, setExpanded] = useState<string[]>([selectedGroupId || 'access']);
  const [followedGroupId, setFollowedGroupId] = useState(selectedGroupId);
  if (selectedGroupId !== followedGroupId) {
    setFollowedGroupId(selectedGroupId);
    if (selectedGroupId) setExpanded([selectedGroupId]);
  }
  const navigate = (href: string) => {
    if (onNavigate) void onNavigate(href);
    else window.location.assign(href);
  };
  return (
    <nav data-sidebar-navigation="settings" aria-label={msg('nav.settingsNavigation', 'Settings navigation')}>
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton
            aria-label={msg('nav.backToApp', 'Back to app')}
            tooltip={msg('nav.backToApp', 'Back to app')}
            onClick={() => navigate(returnHref)}
          >
            <ArrowLeft className="size-4" aria-hidden />
            {!compact && <span>{msg('nav.backToApp', 'Back to app')}</span>}
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
      {compact ? (
        <SidebarMenu className="mt-3">
          {groups.map((group) => (
            <CompactNavigationGroup
              key={group.id}
              label={msg(group.labelId, group.label)}
              icon={group.icon}
              items={group.children}
              currentPath={currentPath}
              onNavigate={onNavigate}
            />
          ))}
        </SidebarMenu>
      ) : (
        <Accordion
          className="settings-accordion mt-3 gap-2"
          value={expanded}
          onValueChange={(value) => setExpanded(value as string[])}
        >
          {groups.map((group) => {
            const GroupIcon = group.icon;
            return (
              <AccordionItem value={group.id} key={group.id} className="border-0">
                <AccordionTrigger
                  render={
                    <SidebarMenuButton isActive={selected?.group.id === group.id && !expanded.includes(group.id)} />
                  }
                >
                  <GroupIcon className="size-4" aria-hidden />
                  <span className="flex-1 truncate">{msg(group.labelId, group.label)}</span>
                </AccordionTrigger>
                <AccordionContent className="pb-0">
                  <SidebarMenuSub className="mx-0 translate-x-0 border-0 px-0" data-settings-navigation-children="true">
                    {group.children.map((item) => {
                      const ItemIcon = item.icon;
                      return (
                        <SidebarMenuSubItem key={item.href}>
                          <SidebarMenuSubButton
                            render={<a href={navigationHref(item.href, activeWorkspaceId)} />}
                            onClick={(event) => {
                              if (!isPlainNavigationClick(event)) return;
                              event.preventDefault();
                              navigate(navigationHref(item.href, activeWorkspaceId));
                            }}
                            aria-current={isActivePath(currentPath, item.href) ? 'page' : undefined}
                            isActive={isActivePath(currentPath, item.href)}
                            title={msg(item.labelId, item.label)}
                          >
                            {ItemIcon && <ItemIcon className="size-4" aria-hidden />}
                            <span className="flex-1 truncate">{msg(item.labelId, item.label)}</span>
                          </SidebarMenuSubButton>
                        </SidebarMenuSubItem>
                      );
                    })}
                  </SidebarMenuSub>
                </AccordionContent>
              </AccordionItem>
            );
          })}
        </Accordion>
      )}
    </nav>
  );
}

export function SettingsAccess({
  currentPath,
  account,
  children,
}: {
  currentPath: string;
  account?: AuthAccount | null;
  children: ReactNode;
}) {
  const { orgUuid } = useWorkspace();
  const { msg } = useI18n();
  const item = settingsItem(currentPath);
  if (item && !canAccessSettings(item.href, account, orgUuid))
    return (
      <AccessDenied
        title={msg('common.accessDenied', 'Access denied')}
        description={msg('settings.noAccess', 'You do not have permission to manage these organization settings.')}
      />
    );
  return children;
}

export function SettingsLocation({ currentPath, account }: { currentPath: string; account?: AuthAccount | null }) {
  const { msg } = useI18n();
  const { orgUuid, activeWorkspace } = useWorkspace();
  const organizationAccess = useContext(AuthContext)?.organizationAccess;
  const item = settingsItem(currentPath);
  const scope = settingScope(currentPath);
  const name =
    scope === 'personal'
      ? account?.email_address
      : scope === 'organization'
        ? currentOrganizationName(account, organizationAccess, orgUuid)
        : localizedWorkspaceName(activeWorkspace.name, msg);
  return (
    <div className="settings-location">
      <span className="truncate">
        {msg('nav.settings', 'Settings')}
        {item && (
          <>
            {' '}
            / {msg(item.group.labelId, item.group.label)} / {msg(item.labelId, item.label)}
          </>
        )}
      </span>
      <span className="truncate" title={name}>
        {msg(
          `settings.scope.${scope}`,
          scope === 'personal' ? 'Account' : scope === 'organization' ? 'Organization' : 'Workspace',
        )}
        : {name}
      </span>
    </div>
  );
}
