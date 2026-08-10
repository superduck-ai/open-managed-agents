---
title: "Managed Agents UI"
summary: "Managed agents interface provides quickstart, agents, sessions, deployments, environments, and resource management pages."
topics: [architecture]
sources:
  - id: managed-agents-page
    type: file
    path: web/src/features/managed-agents/ManagedAgentsPage.tsx
  - id: resources-page
    type: file
    path: web/src/features/managed-agents/resources/ManagedResources.tsx
  - id: quickstart-page
    type: file
    path: web/src/features/managed-agents/quickstart/AgentQuickstartPage.tsx
  - id: agents-page
    type: file
    path: web/src/features/managed-agents/agents/AgentsResourcePage.tsx
  - id: agents-detail
    type: file
    path: web/src/features/managed-agents/agents/detail.tsx
  - id: skills-page
    type: file
    path: docs/design/fe/skills-page.md
---

## Managed Agents UI

The managed agents interface provides pages for quickstarting agents, managing agent configurations, viewing session history, and managing deployments, environments, and supporting resources. The `ManagedAgentsPage` component routes between different sections based on URL state [@managed-agents-page].

## Page Routing

The `ManagedAgentsPage` component accepts a `section` prop that determines which managed agent feature to render:
- `quickstart`: Agent quickstart and getting started guide
- `agents`: Agent configuration list and detail views
- `sessions`: Session history and detail views
- `deployments`: Deployment management
- `environments`: Environment configuration
- `credential-vaults`: Credential vault management
- `memory-stores`: Memory store configuration
- `dreams`: Agent dreaming feature
- `resources`: Generic resource page template

Each section has its own implementation component that receives the current workspace context from the `useWorkspace` hook.

## Quickstart Section

The quickstart section renders the `AgentQuickstartPage` component [@quickstart-page], which provides:
- Getting started documentation with code examples
- Quickstart templates for common agent patterns
- Syntax-highlighted code blocks for agent configuration
- Links to related resources and documentation

The quickstart content includes YAML and JSON code blocks rendered with Highlight.js for syntax highlighting, following the frontend conventions for code display.

## Agents Section

The agents section renders `AgentsResourcePage` for agent configuration management [@agents-page]. The page provides:
- **List view**: Table of agent configurations with status indicators
- **Detail view**: Agent configuration editor with tabs for different sections [@agents-detail]
- **Create dialog**: Form for creating new agent configurations

The agent detail view uses `AgentConfigEditor` component for editing agent configurations, with tabs for:
- **Agent tab**: Name, description, system prompt, and model settings
- **Tools tab**: MCP servers and custom tool configuration
- **Skills tab**: Skill selection and configuration
- **MCPs and tools tab**: Tool permission policies and MCP server connections
- **Metadata tab**: Custom metadata key-value pairs

The detail view displays the skills section with a collapsible list that shows skill name, source badge, and version information, expanding to reveal full metadata per the skills page design spec [@skills-page].

## Sessions Section

The sessions section renders the session list and detail views. The list shows session history with filters for status, date range, and tags. Each session row displays:
- Session ID and title
- Status indicator (running, completed, failed, archived)
- Model and agent information
- Creation and update timestamps
- Action buttons for detail, archive, and delete

Clicking a session navigates to the session detail page with lane-based timeline and event inspection.

## Generic Resource Pages

The `ManagedResourcePage` component provides a generic template for resource list and detail pages [@resources-page]. It uses `ResourceConfig` objects to define:
- API endpoints for list, detail, create, update, and delete operations
- Table columns for the list view
- Detail view structure and edit capabilities
- Permission requirements for operations

Resource configurations include:
- **Deployments**: Agent deployment configurations and status
- **Environments**: Runtime environment settings
- **Credential vaults**: Stored credentials for agent tools
- **Memory stores**: Vector database configurations for memory

## Workspace Context

All managed agent pages use the `useWorkspace` hook to access the current workspace context. The `activeWorkspaceId` is used for:
- API request scoping
- URL generation for workspace-scoped routes
- Workspace switching when URL contains different workspace ID

The `ManagedAgentsPage` effect synchronizes the route workspace ID with the active workspace, automatically switching workspaces when navigating to a workspace-scoped URL.

## Notifications

The managed agents pages use Sonner for toast notifications, with the `Toaster` component rendered at the page level. Toasts display success, error, and informational messages for:
- Create/update/delete operations
- Validation errors
- API connection issues

## Error Handling

Managed agent pages display errors using consistent alert components:
- **ManagedErrorAlert**: Destructive-styled alert for blocking errors
- **ManagedWarningAlert**: Warning-styled alert for non-blocking issues
- **ConfirmEntityDialog**: Confirmation dialog for destructive actions

Error messages are derived from API error responses with fallback to generic error text.
