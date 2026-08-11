---
title: "shadcn/ui Frontend"
summary: "The frontend uses shadcn/ui components with the new-york style, Base UI primitives, and Tailwind CSS for the console interface."
topics: [frontend, architecture]
sources:
  - id: web-conventions
    type: file
    path: web/AGENTS.md
  - id: shadcn-theme
    type: file
    path: web/design/shadcn-theme.md
  - id: shadcn-components
    type: file
    path: web/design/shadcn-components.md
---

The frontend console uses shadcn/ui components as the primary UI library, adopting the `new-york` style variant with Base UI primitives and Tailwind CSS styling. This combination provides a complete component system while maintaining product semantics, API compatibility, and architectural boundaries.

## Component Library

The project prioritizes official shadcn/ui components over custom implementations for common UI patterns. Standard controls including accordion, alert, alert dialog, avatar, badge, breadcrumb, button, button group, card, checkbox, collapsible, combobox, command menu, context menu, data table, dialog, drawer, dropdown menu, field, input, input group, label, pagination, popover, progress, radio group, scroll area, select, separator, sheet, skeleton, slider, spinner, switch, table, tabs, textarea, toggle, toggle group, and tooltip come from the shadcn registry [@web-conventions].

The shadcn ecosystem provides specialized components for conversational interfaces: attachment for file display, bubble for message content, marker for inline status, message for conversation turns, and message scroller for chat scroll containers [@shadcn-components].

## Architecture and Tooling

The frontend is a Vite application using React, TypeScript, and TanStack Router. State management uses TanStack Query, while dense data tables employ TanStack Table. TanStack Form is reserved only for complex form scenarios [@web-conventions].

Bun manages the package registry, scripts, testing, and build process. The development workflow uses `bun install`, `bun run dev`, `bun run build`, and `bun test`. Production artifacts from `web/dist` are served as static files by Nginx or another static server [@web-conventions].

## Styling System

Tailwind CSS with semantic CSS variable tokens provides theming. The `new-york` variant defines compact controls, restrained borders, clear focus rings, and complete light and dark theme alignment. Tokens such as `background`, `foreground`, `card`, `popover`, `primary`, `secondary`, `muted`, `accent`, `destructive`, `border`, `input`, and `ring` provide semantic color abstraction [@shadcn-theme].

Radius values scale from a single `--radius` base value. Derived tokens include `radius-sm`, `radius-md`, `radius-lg`, `radius-xl`, `radius-2xl`, `radius-3xl`, and `radius-4xl` for consistent corner sizing across components [@shadcn-theme].

## Base UI Integration

Base UI serves as the unstyled primitive layer compatible with shadcn. The project avoids scattering raw `@base-ui/react` imports throughout feature code. Common primitives are wrapped in `src/shared/ui/` or use generated shadcn components that encapsulate Base UI behavior [@web-conventions].

Generated shadcn components stay within the repository. Project tokens are adapted where needed, but components are not forked into feature-specific one-off versions [@web-conventions].

## Design Conventions

Visual direction follows the shadcn `new-york` aesthetic. Marketing landing pages are avoided in favor of a functional console interface. Icon buttons are preferred for common actions. Nested cards and decorative gradients are minimized. Button, cell, and menu text must not overflow on narrow screens [@web-conventions].

Popover and menu rendering uses portal mounting to appear above navigation and content without being clipped by scroll containers. Keyboard and pointer interactions follow ARIA patterns for menus, dialogs, and selects [@web-conventions].

## Component Patterns

The project does not recreate behaviors that shadcn and Base UI already provide. Portal mounting, popover positioning, click-outside-to-close, Escape handling, focus restoration, keyboard navigation, ARIA roles, switch semantics, and tab roving focus rely on existing implementations rather than custom code [@web-conventions].

When shadcn lacks an exact match, the closest official components are combined with a minimal adaptation layer. Feature-specific UI code does not import raw Base UI primitives directly [@web-conventions].

## API Boundaries

The frontend maintains explicit separation between `/api/*` Console API routes and `/v1/*` Anthropic-compatible API routes. Each boundary uses its own client. Console API requests require credentials and `X-CSRF-Token` for mutations. Files API requests include the `anthropic-beta: files-api-2025-04-14` header and `?beta=true` parameter [@web-conventions].

Authentication uses cookie-based sessions with platform bootstrap as the entry point. Session keys are not stored in `localStorage` or `sessionStorage`. 401 responses clear local auth state and redirect to login, while 403 responses preserve the session and display permission errors [@web-conventions].
