---
title: "Frontend Routing"
summary: "TanStack Router provides type-safe routing with route guards for authentication and protected console navigation."
topics: [architecture]
sources:
  - id: router
    type: file
    path: web/src/app/router.tsx
  - id: protected-layout
    type: file
    path: web/src/app/layout/ProtectedConsoleLayout.tsx
  - id: console-layout
    type: file
    path: web/src/app/layout/ConsoleLayout.tsx
  - id: agents-md
    type: file
    path: web/AGENTS.md
---

## Frontend Routing

The application uses TanStack Router for type-safe, file-based routing with route guards for authentication and protected navigation. Routes are defined in `web/src/app/router.tsx` using a nested route structure [@router] that supports authentication checks and console layout inheritance.

## Route Structure

Routes follow a hierarchical pattern with three main levels:
- **Root route**: Base route that renders an outlet for child routes
- **Protected route**: Authentication guard that redirects unauthenticated users to login
- **Console route**: Main application layout with sidebar navigation

The protected route uses `ProtectedConsoleLayout` to check authentication status before allowing access [@protected-layout]. Unauthenticated users are redirected to `/login` with a return-to parameter for post-login navigation.

## Route Definitions

Routes are created using TanStack Router's route creation functions:
```tsx
const rootRoute = createRootRoute({ component: () => <Outlet /> });
const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'protected',
  component: ProtectedConsoleLayout
});
const consoleRoute = createRoute({
  getParentRoute: () => protectedRoute,
  id: 'console',
  component: ConsoleLayout
});
```

Child routes are added to create the full route tree, which is then passed to `createRouter`.

## Authentication Flow

The `ProtectedConsoleLayout` component checks the authentication status from the auth context:
- **Loading**: Shows a loading state while bootstrap request completes
- **Anonymous**: Redirects to `/login` with the current location for post-login redirect
- **Authenticated**: Renders child routes via `<Outlet />`

This ensures all console routes are only accessible to authenticated users.

## Console Layout

The `ConsoleLayout` component provides the main application shell with sidebar navigation and workspace switching [@console-layout]. It handles:
- Workspace switching and persistence
- Navigation between console sections
- Mobile-responsive sidebar behavior
- User account menu and logout

The layout uses `navigationHref` to generate workspace-scoped URLs for features that require workspace context.

## Route Patterns

The application supports several URL patterns:
- **Global routes**: `/workbench`, `/api-keys`, `/webhooks` (active workspace defaults)
- **Workspace-scoped routes**: `/workspaces/{workspaceId}/(playground|files|skills|batches|agent-quickstart|agents|sessions)`
- **Settings routes**: `/settings/(organization|members|service-accounts|limits)`
- **Analytics routes**: `/usage`, `/usage/cache`, `/usage/limits`, `/cost`, `/logs`

## Search Parameter Validation

Routes use `validateSearch` for type-safe search parameters:
```tsx
const sessionDetailSearch = (search: Record<string, unknown>) => ({
  segment: search.segment === 'debug' ? 'debug' : undefined,
  event: typeof search.event === 'string' && search.event.trim() ? search.event.trim() : undefined
});
```

This ensures URL state is properly typed and validated before use in components.

## Navigation

Components use TanStack Router's `useNavigate` hook for programmatic navigation:
```tsx
const navigate = useNavigate();
await navigate({ to: '/login', search: { returnTo: '/' } });
```

The navigation system integrates with workspace selection to automatically update URLs when switching workspaces.

## Route Guards

Per the frontend conventions, route guards enforce:
- Bootstrap completion before protected route access [@agents-md]
- Permission checks for gated features (implemented in components)
- Workspace validation for workspace-scoped routes

Route guards are implemented at the layout level rather than in individual route components for consistency.
