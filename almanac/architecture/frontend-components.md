---
title: "Frontend Components"
summary: "The frontend uses shadcn/ui components with a custom new-york theme for consistent, accessible UI across the application."
topics: [architecture]
sources:
  - id: agents-md
    type: file
    path: web/AGENTS.md
  - id: shared-ui
    type: file
    path: web/src/shared/ui/
  - id: app-providers
    type: file
    path: web/src/app/App.tsx
---

## Frontend Components

The Open Managed Agents frontend uses a component system built on shadcn/ui with a custom `new-york` theme configuration. All UI components are located in `web/src/shared/ui/` and follow accessibility best practices with ARIA roles and keyboard navigation support.

## Component Library

The application uses shadcn/ui components as the foundation, with modifications to align with the product's visual design per the frontend conventions [@agents-md]. Core UI components are located in `web/src/shared/ui/` [@shared-ui] and include buttons, inputs, dialogs, sheets (drawers), dropdown menus, tables, tabs, and form controls like switches, checkboxes, and radio groups.

The component system prioritizes:
- **Accessibility**: All interactive components include proper ARIA roles and keyboard navigation
- **Theme support**: Full dark/light mode with semantic CSS variables
- **Composability**: Components can be combined to build complex interfaces
- **Responsive design**: Components work across mobile and desktop viewports

## Design System

The visual design follows the shadcn `new-york` variant with:
- Compact controls with minimal borders
- Semantic color tokens for backgrounds, foregrounds, and accents
- Consistent border radius and focus rings across all controls
- Optimized dark and light theme alignment

Theme tokens use CSS variables for colors like `background`, `foreground`, `card`, `popover`, `primary`, `secondary`, `muted`, `accent`, `destructive`, `border`, `input`, and `ring`.

## Component Organization

Shared UI components are organized by type:
- **Form controls**: `button.tsx`, `input.tsx`, `textarea.tsx`, `select.tsx`, `checkbox.tsx`, `switch.tsx`, `radio-group.tsx`, `slider.tsx`
- **Layout**: `sidebar.tsx`, `sheet.tsx`, `dialog.tsx`, `popover.tsx`, `resizable.tsx`
- **Navigation**: `tabs.tsx`, `breadcrumb.tsx`, `dropdown-menu.tsx`, `collapsible.tsx`, `toggle.tsx`
- **Feedback**: `alert.tsx`, `alert-dialog.tsx`, `toast.tsx` (via sonner), `skeleton.tsx`, `badge.tsx`
- **Data display**: `table.tsx`, `card.tsx`, `separator.tsx`, `empty.tsx`
- **Overlays**: `tooltip.tsx`, `command.tsx`

## Usage Pattern

Feature code imports components directly from `@/shared/ui`:
```tsx
import { Button } from '@/shared/ui/button';
import { Dialog } from '@/shared/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/shared/ui/tabs';
```

The app is wrapped with providers that supply theme and internationalization context to all components [@app-providers].

## Customization

While built on shadcn/ui, components include custom styling for:
- Custom scrollbars (`subtle-scrollbar` class)
- Product-specific color schemes
- Motion preferences (reduced motion support)
- Console-specific layout patterns

Components are generated into the repository rather than installed as dependencies, allowing for project-specific adaptations while maintaining consistency with the shadcn ecosystem.
