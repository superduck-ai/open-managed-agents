---
title: "Adding Frontend Feature"
summary: "Guide for implementing new features in the Open Managed Agents web console using React, TypeScript, and shadcn/ui."
topics: [frontend, development]
sources:
  - id: web-agents-md
    type: file
    path: web/AGENTS.md
  - id: web-features-dir
    type: file
    path: web/src/features/
  - id: web-shared-dir
    type: file
    path: web/src/shared/
  - id: web-package-json
    type: file
    path: web/package.json
  - id: managed-agents-page
    type: file
    path: web/src/features/managed-agents/ManagedAgentsPage.tsx
  - id: managed-resources
    type: file
    path: web/src/features/managed-agents/resources.tsx
  - id: workbench-page
    type: file
    path: web/src/features/workbench/WorkbenchPage.tsx
  - id: router
    type: file
    path: web/src/app/router.tsx
---

The Open Managed Agents frontend is a static Vite application built with React, TypeScript, and Tailwind CSS, served by a Go backend. Feature implementation follows vertical slice architecture with shadcn/ui components and TanStack libraries for routing and state management.

## Tech Stack

The frontend uses Bun as its package manager and runtime tool. Key dependencies include React for components, TanStack Router for routing, TanStack Query for server state, and TanStack Table for data grids[@web-package-json]. Form state uses TanStack Form only when complexity warrants it. UI components prioritize shadcn/ui's `new-york` variant with Base UI as the unstyled foundation.

## Project Structure

Feature code lives in `web/src/features/` organized by vertical domain slices: `quickstart/`, `agents/`, `sessions/`, `resources/`, and `settings/`[@web-agents-md]. Each feature owns its pages, components, API clients, and state logic. Shared abstractions reside in `web/src/shared/` for cross-cutting concerns like authentication, API clients, permissions, and UI primitives[@web-shared-dir].

## Creating a New Feature

Start by creating a feature directory under `web/src/features/`. A typical feature includes a page component, API client hooks, models, and subcomponents. For example, the managed agents feature separates concerns into `ManagedAgentsPage.tsx`, `api.ts`, `model.tsx`, and feature-specific components like `resources.tsx`[@web-features-dir][@managed-agents-page][@managed-resources].

Page components use route guards for authentication and authorization, as seen in `ManagedAgentsPage` and `WorkbenchPage` [@managed-agents-page][@workbench-page]. Protected routes require successful bootstrap completion before access. Permission-gated routes render an access-denied state when authenticated users lack required permissions. Routes are registered in the TanStack Router configuration[@router]. Route files should not directly couple to raw fetch calls—all API interactions go through clients in `src/shared/api/`[@web-agents-md].

## UI Components

Use official shadcn/ui components before building custom controls. Common patterns include `Accordion`, `Alert Dialog`, `Button`, `Card`, `Data Table`, `Dialog`, `Input`, `Select`, `Tabs`, and `Tooltip`[@web-agents-md]. Components are generated into the repository and adapted with project tokens. Feature code should not scatter raw `@base-ui/react` imports—generic primitives are encapsulated in `src/shared/ui/`.

The visual style follows shadcn's `new-york` conventions: compact controls, minimal borders, semantic tokens, and complete light/dark theme support. Semantic tokens like `background`, `foreground`, `card`, `popover`, `primary`, and `muted` replace project-specific aliases.

## API Clients

API clients in `src/shared/api/` handle `/api` console routes and `/v1` Anthropic-compatible routes separately. Console API requests require credentials and CSRF tokens on state-changing requests. `/v1/files` requests include the `anthropic-beta: files-api-2025-04-14` header[@web-agents-md]. Errors are normalized at the client boundary into a small frontend error type.

Streaming responses use helpers in `src/shared/api/streaming.ts`. The streaming implementation supports POST bodies, cancellation, incremental event parsing, and authentication handling. It avoids forcing SSE streams into TanStack Query[@web-agents-md].

## Authentication and Authorization

Frontend authentication begins with `/api/bootstrap`, which returns account, organization, workspace, permissions, and CSRF token stored in the authentication context. Session identity relies on cookies—never `localStorage` or `sessionStorage`. All state-changing requests require `X-CSRF-Token`[@web-agents-md].

Role-based access control uses these role values: `user`, `claude_code_user`, `developer`, `billing`, and `admin`[@web-agents-md]. Frontend permission checks only affect UX—the backend RBAC system is authoritative. Permission helpers live in `src/shared/permissions/`.

## Testing

Tests are placed near the code they exercise. Use Bun test for utilities, API clients, permissions, and component logic. Playwright handles browser flows for end-to-end testing[@web-agents-md]. Before committing changes, run `bun test` and `bun run build`. UI changes affecting layout, responsiveness, or interaction fidelity require browser verification.

## Local Development

Start the backend server from the repository root, then start the frontend from `web/` with `bun run dev`. The Vite development server proxies `/api` and `/v1` requests to the Go backend[@web-agents-md]. After modifying frontend code, restart the frontend dev server via `./restart-web.sh` before browser verification.

Production builds generate static assets in `web/dist/` served by Nginx or another static server. The build process runs TypeScript compilation followed by Vite bundling[@web-package-json].
