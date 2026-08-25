import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { router } from './router';
import { AuthProvider } from '../shared/auth/AuthProvider';
import { I18nProvider, useI18n } from '../shared/i18n';
import { ThemeProvider } from '../shared/theme/ThemeProvider';
import { Toaster } from '../shared/ui/sonner';
import { WorkspaceProvider } from '../shared/workspaces/WorkspaceProvider';

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
            <WorkspaceProvider>
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
