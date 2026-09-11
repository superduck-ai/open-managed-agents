import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { router } from './router';
import { AuthProvider } from '../shared/auth/AuthProvider';
import { I18nProvider, useI18n } from '../shared/i18n';
import { ThemeProvider } from '../shared/theme/ThemeProvider';
import { Toaster } from '../shared/ui/sonner';
import { WorkspaceProvider } from '../shared/workspaces/WorkspaceProvider';
import { workspaceIdFromPath, workspaceSwitchPath } from '../shared/workspaces/presentation';

const initialWorkspaceId = workspaceIdFromPath(window.location.pathname);

async function navigateScope(workspaceId: string) {
  const href = workspaceSwitchPath(router.state.location.pathname, workspaceId);
  if (href === router.state.location.pathname) return;
  await router.navigate({ href, replace: true, ignoreBlocker: true });
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 15_000,
    },
  },
});

export function App() {
  return (
    <I18nProvider>
      <ThemeProvider>
        <AppToaster />
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <WorkspaceProvider navigateScope={navigateScope} initialWorkspaceId={initialWorkspaceId}>
              <RouterProvider router={router} />
            </WorkspaceProvider>
          </AuthProvider>
        </QueryClientProvider>
      </ThemeProvider>
    </I18nProvider>
  );
}

function AppToaster() {
  const { msg } = useI18n();

  return (
    <Toaster
      duration={4000}
      closeButton
      containerAriaLabel={msg('common.notifications', 'Notifications')}
      toastOptions={{ closeButtonAriaLabel: msg('common.close', 'Close') }}
    />
  );
}
