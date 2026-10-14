import { Outlet, useLocation, useNavigate } from '@tanstack/react-router';
import { useEffect, useRef } from 'react';
import { useAuth } from '../../shared/auth/context';
import { normalizeReturnTo } from '../../shared/auth/redirects';
import { useOrganizations } from '../../shared/organizations/context';
import { useWorkspace } from '../../shared/workspaces/context';
import { ConsoleShell } from './ConsoleLayout';
import { Button } from '../../shared/ui/button';

export function ProtectedConsoleLayout() {
  const { status, account, logout, refresh } = useAuth();
  const organizations = useOrganizations();
  const { activeWorkspaceId } = useWorkspace();
  const location = useLocation();
  const navigate = useNavigate();
  const redirecting = useRef(false);
  useEffect(() => {
    if (status !== 'anonymous' || redirecting.current || location.pathname === '/login') return;
    redirecting.current = true;
    void navigate({ to: '/login', search: { returnTo: normalizeReturnTo(location.href) }, replace: true });
  }, [location.href, location.pathname, navigate, status]);

  if (status === 'loading') {
    return (
      <div className="grid min-h-screen place-items-center bg-background text-foreground">
        <div className="text-sm text-muted-foreground">Loading Open Managed Agents...</div>
      </div>
    );
  }

  if (status === 'anonymous') {
    return null;
  }
  if (status === 'error')
    return (
      <div role="alert" className="p-8">
        登录信息加载失败。<Button onClick={() => void refresh().catch(() => undefined)}>重试</Button>
      </div>
    );

  if (
    organizations &&
    (organizations.switching || organizations.error || !organizations.orgUuid || !activeWorkspaceId)
  ) {
    return (
      <ConsoleShell account={account} onLogout={logout} currentPath={location.pathname}>
        <div role={organizations.error ? 'alert' : 'status'} className="space-y-3">
          <p>
            {organizations.switching
              ? '正在加载组织…'
              : organizations.error
                ? '加载组织失败，请重试。'
                : '暂无可用工作区。请选择组织或接受邀请。'}
          </p>
          {!organizations.switching && (
            <Button variant="outline" onClick={() => void organizations.retry()}>
              重试
            </Button>
          )}
        </div>
      </ConsoleShell>
    );
  }
  return <Outlet key={`${organizations?.orgUuid ?? ''}:${activeWorkspaceId}`} />;
}
