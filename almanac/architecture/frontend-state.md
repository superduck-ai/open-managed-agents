---
title: "Frontend State"
summary: "React Context and TanStack Query manage server state with auth, workspace, and theme providers for global application state."
topics: [architecture]
sources:
  - id: auth-provider
    type: file
    path: web/src/shared/auth/AuthProvider.tsx
  - id: workspace-provider
    type: file
    path: web/src/shared/workspaces/WorkspaceProvider.tsx
  - id: theme-provider
    type: file
    path: web/src/shared/theme/ThemeProvider.tsx
  - id: i18n-provider
    type: file
    path: web/src/shared/i18n/I18nProvider.tsx
  - id: app
    type: file
    path: web/src/app/App.tsx
  - id: agents-md
    type: file
    path: web/AGENTS.md
---

## Frontend State

The application uses React Context for global client-side state and TanStack Query for server state. Providers are nested in `App.tsx` to supply authentication, workspace, theme, and internationalization contexts throughout the application [@app].

## Authentication State

The `AuthProvider` wraps the application and manages user authentication through a bootstrap request to `/api/bootstrap` [@auth-provider]. It provides:

- **Account information**: User profile data from the bootstrap response
- **Authentication status**: `loading`, `authenticated`, or `anonymous`
- **CSRF token**: Token for authenticated requests
- **Logout function**: Clears auth state and redirects to login

The provider uses TanStack Query to fetch and cache bootstrap data with no retries on failure. Authentication status determines whether protected routes render or redirect to login.

## Workspace State

The `WorkspaceProvider` manages the active workspace context for features that operate within workspace scope [@workspace-provider]. It provides:

- **Workspaces list**: Available workspaces for the current organization
- **Active workspace**: Currently selected workspace (defaults to 'default')
- **Active workspace ID**: ID used in workspace-scoped API requests
- **Selection function**: Switches active workspace and persists preference
- **Creation function**: Creates new workspaces and updates the list
- **Refresh function**: Refetches the workspaces list

The workspace preference is persisted to `localStorage` and synchronized with URL paths containing workspace IDs. The provider also sets request context headers for API calls.

## Server State

TanStack Query manages server state with a global `QueryClient` configured per the frontend conventions [@agents-md] with:
- `refetchOnWindowFocus: false`: Prevents automatic refetch on window focus
- `retry: 1`: Retries failed requests once
- `staleTime: 15000`: Considers data fresh for 15 seconds

This configuration balances data freshness with performance for the console application.

## Theme State

The `ThemeProvider` manages the application theme mode (light, dark, or system) and resolves the actual theme to apply [@theme-provider]. It:

- Reads the theme preference from `localStorage`
- Listens for system theme changes when in 'system' mode
- Applies theme classes and data attributes to `document.documentElement`
- Persists theme changes to storage

Theme state affects CSS variable resolution for component styling.

## Internationalization State

The `I18nProvider` manages the application locale and message formatting using react-intl [@i18n-provider]. It provides:

- **Current locale**: Active language code (defaults to browser preference)
- **Message function**: Formats translated messages with values
- **Locale setter**: Changes the active locale and persists to storage

Supported locales include English (`en`) and Simplified Chinese (`zh-CN`), with message catalogs stored as JSON files.

## State Updates

Server state is updated through TanStack Query's mutations and cache invalidation. Client state updates typically follow this pattern:

```tsx
queryClient.setQueryData<WorkspaceType>(
  ['console', 'workspaces', orgUuid],
  (current) => [...current, created]
);
```

This optimistic updating keeps the UI responsive while server changes are persisted.

## Request Context

The workspace provider sets request context that includes organization and workspace IDs for API calls. This context is attached to outbound requests via the API client layer, ensuring console API calls include the correct scope headers.

## State Persistence

The application persists several pieces of state to `localStorage`:
- Active workspace ID (`oma.activeWorkspaceId`)
- Theme mode preference (`oma.theme.mode`)
- Locale preference (`oma.locale`)

These preferences restore the user's last chosen settings across sessions.
