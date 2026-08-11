---
title: "Frontend Architecture"
summary: "Architectural overview of the Open Managed Agents frontend, including routing patterns, state management, and component organization."
topics: [architecture, frontend]
sources:
  - id: web-agents
    type: file
    path: web/AGENTS.md
  - id: router
    type: file
    path: web/src/app/router.tsx
---

The Open Managed Agents frontend is a static console application built with React, TypeScript, and Vite. It follows a feature-oriented architecture with clear separation between routing, state management, and UI components.

## Technology Stack

**Core frameworks and libraries**:
- **Bun** - Package manager and build tool
- **Vite** - Build tool and dev server with HMR
- **React** - UI framework
- **TypeScript** - Type safety
- **TanStack Router** - File-based routing with route guards
- **TanStack Query** - Server state management
- **shadcn/ui** - UI component library (new-york variant)
- **Tailwind CSS** - Styling with semantic CSS variables
- **lucide-react** - Icon library

The frontend produces static files in `web/dist/` served by Caddy or another static web server [@web-agents].

## Project Structure

```
web/src/
├── app/                    # Application setup and routing
│   ├── App.tsx             # Root component
│   ├── layout/             # Layout components
│   ├── router.tsx          # Route definitions
│   └── main.tsx            # Entry point
├── features/               # Feature-specific code
│   ├── auth/               # Login and authentication
│   ├── dashboard/          # Dashboard pages
│   ├── managed-agents/     # Agents, sessions, deployments
│   ├── settings/           # Settings pages
│   └── workbench/          # Workbench interface
├── shared/                 # Shared utilities
│   ├── api/                # API clients
│   ├── auth/               # Authentication context
│   ├── i18n/               # Internationalization
│   ├── permissions/        # Permission helpers
│   ├── ui/                 # Shared UI components
│   └── workspaces/         # Workspace context
└── styles.css              # Global styles
```

Features are organized vertically with co-located tests, API clients, and components.

## Routing Architecture

**TanStack Router** provides file-based routing with type-safe navigation. Routes are defined in `router.tsx` with hierarchical structure [@router]:

```tsx
const rootRoute = createRootRoute({
  component: () => <Outlet />
});

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

**Route hierarchy**:
- `root` - Application root
- `protected` - Authenticated route guard
- `console` - Main console layout
- Leaf routes - Specific pages (dashboard, workbench, agents, etc.)

**Workspace-scoped routes** support multi-tenancy:

```tsx
const workspaceAgentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/agents',
  component: () => <ManagedAgentsPage section="agents" />
});
```

The route tree is built additively with clear parent-child relationships.

## Authentication Flow

Authentication begins with `/api/bootstrap` which returns account, organization, workspace, permissions, and CSRF token. This data is stored in the auth context and used for authorization checks [@web-agents].

**Auth context** (`shared/auth/context.ts`):
- Stores bootstrap data
- Provides login/logout functions
- Exposes permissions for role-based access control

**Session management**:
- Session identity based on cookies (not localStorage)
- CSRF token required for mutating requests
- `401` responses trigger redirect to login
- `403` responses show access-denied state

**Route guards**:
- Protected routes require bootstrap success
- Permission-gated routes show access-denied for unauthorized users
- Login route redirects authenticated users to console

## State Management

The frontend uses multiple state management approaches for different needs:

**TanStack Query** manages server state:
- Cached API responses with automatic refetching
- Optimistic updates for better UX
- Loading and error state handling

**React Context** manages global state:
- `AuthProvider` - Authentication data
- `WorkspaceProvider` - Current workspace selection
- `I18nProvider` - Translations and locale

**Local component state** manages UI state:
- Form inputs and validation
- UI toggles and selections
- Temporary interactive states

## API Client Architecture

API clients are organized in `shared/api/` with separate clients for different API surfaces [@web-agents]:

**Console API client** (`/api/*`):
- Requires credentials (cookies)
- Mutating requests need `X-CSRF-Token`
- JSON request/response format

**Anthropic-compatible API** (`/v1/*`):
- SDK-compatible error responses
- Beta headers for new features
- File upload support with multipart

**Streaming helper** (`shared/api/streaming.ts`):
- SSE event processing
- Abort/cancel support
- Incremental event parsing

**Error normalization** at API client boundaries converts diverse API errors into a small set of frontend error types.

## Component Architecture

**shadcn/ui components** from `shared/ui/` provide the base layer. These are generated versions of official shadcn components adapted to project tokens [@web-agents]:

```tsx
import { Button } from '@/shared/ui/button';
import { Dialog, DialogContent, DialogTrigger } from '@/shared/ui/dialog';
```

**Feature components** in `features/*/` compose UI components into domain-specific interfaces:
- Pages - Full-screen route components
- Components - Reusable feature-specific components
- Hooks - Custom React hooks for feature logic

**Layout components** in `app/layout/` provide application structure:
- `ProtectedConsoleLayout` - Authenticated layout shell
- `ConsoleLayout` - Main console navigation and content area

## Feature Organization

The `features/` directory contains feature-specific implementations:

**Dashboard** (`features/dashboard/`):
- Home page with overview
- Analytics sections (usage, caching, rate limits, cost, logs)
- Privacy controls and security settings

**Workbench** (`features/workbench/`):
- Prompt editor with streaming responses
- Evaluation and testing tools
- File upload and management

**Managed Agents** (`features/managed-agents/`):
- Quickstart guides
- Agent, session, deployment, and environment management
- Resource pages with configuration-driven tables

**Settings** (`features/settings/`):
- Organization settings (members, service accounts, limits)
- Workspace settings (API keys, webhooks)

Each feature is self-contained with its own API client, types, and tests.

## Styling System

**Tailwind CSS** with semantic CSS variables provides the styling foundation. The project uses shadcn's new-york design tokens [@web-agents]:

```css
:root {
  --background: 0 0% 100%;
  --foreground: 240 10% 3.9%;
  --card: 0 0% 100%;
  --card-foreground: 240 10% 3.9%;
  /* ... more tokens */
}
```

**Theme support**:
- Light and dark themes from the start
- shadcn semantic tokens (`background`, `foreground`, `primary`, `secondary`, etc.)
- No custom `--oma-*` aliases

**Component styling patterns**:
- Use Tailwind classes for layout
- Use CSS variables for colors
- Avoid nested cards and decorative gradients
- Ensure text doesn't overflow on narrow screens

## Workspace Context

Multi-tenancy is managed through the workspace context (`shared/workspaces/`):

```tsx
const { activeWorkspaceId, selectWorkspace } = useWorkspace();
```

**Workspace switching**:
- URL-based workspace selection (`/workspaces/$id/...`)
- Context propagation to API calls
- Automatic workspace selection from routes

**Workspace isolation**:
- API requests scoped to active workspace
- UI reflects workspace-specific data
- Settings and resources workspace-scoped

## Testing Strategy

**Unit tests** use Bun test for component logic, API clients, and utilities:

```bash
bun test
```

**Browser tests** use Playwright for end-to-end workflows requiring real DOM interaction:

```bash
bun run test:e2e
```

**Test organization**:
- Co-located with source code (`*.test.tsx`)
- Feature-focused test coverage
- CI pipeline runs both unit and browser tests

## Build and Deployment

**Development mode**:

```bash
cd web
bun install
bun run dev
```

Vite dev server with HMR at `http://127.0.0.1:5173` proxies API requests to the backend.

**Production build**:

```bash
bun run build
```

Generates optimized static files in `web/dist/` ready for deployment.

**Deployment**:
- Static files served by Caddy or another web server
- API requests reverse-proxied to backend
- No Node.js runtime required in production
- CDN-friendly static asset delivery

The frontend is designed as a static console application - it doesn't run as a server-side application or BFF, maintaining clear API boundaries with the Go backend.
