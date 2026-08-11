---
title: "Platform API"
summary: "The platform API provides console-oriented routes for organization management, billing, analytics, and platform bootstrapping separate from the Anthropic-compatible service API."
topics: [architecture]
sources:
  - id: platform-routes
    type: file
    path: internal/platformapi/platform_backend_routes.go
  - id: platform-bootstrap
    type: file
    path: internal/platformapi/platform_bootstrap.go
  - id: api-server
    type: file
    path: internal/api/server.go
---

The platform API is a set of HTTP routes that support the platform console web interface rather than external API clients. These routes handle organization and workspace management, billing and usage analytics, platform bootstrapping, and administrative functions. They are registered separately from the Anthropic-compatible `/v1/*` service API routes and use different authentication and response conventions.

## Route Registration

Platform API routes are registered using dedicated functions that organize routes by functional area [@platform-routes]. Route registration happens in the API server setup:

- `RegisterPlatformAccountRoutes`: Bootstrap, banners, and onboarding
- `RegisterPlatformBillingRoutes`: Stripe region and billing configuration
- `RegisterOrganizationOnboardingRoutes`: Onboarding tasks and setup requirements
- `RegisterOrganizationExperienceRoutes`: Feature flags and experiences
- `RegisterOrganizationRootRoutes`: Organization CRUD operations
- `RegisterOrganizationBillingRoutes`: Usage, rate limits, credits, and costs
- `RegisterOrganizationAnalyticsRoutes`: Session analytics and timeseries data
- `RegisterConsoleOrganizationWorkspaceRoutes`: Workspace listing
- `RegisterConsoleOrganizationAdminRequestRoutes`: Admin request listing

These routes are mounted under paths like `/api/*` and `/console/*` rather than `/v1/*`, distinguishing them from the service API [@api-server].

## Bootstrap Endpoint

The `/api/bootstrap` endpoint is the primary entry point for the platform console, returning initialization data for the authenticated user [@platform-bootstrap]. The response includes:

- **Account**: User profile, email, memberships, and organizations
- **Feature flags**: Statsig and Growthbook feature configurations
- **Access control**: User permissions, role, and feature access status
- **Localization**: System prompts and server localization strings

The bootstrap endpoint uses the authenticated principal from the request context to load user data and organization memberships. When an `orgUuid` query parameter is provided, it selects that organization as the preferred context; otherwise, it uses the principal's default organization [@platform-bootstrap].

## Billing and Usage

Platform billing routes provide usage and cost information for the console UI [@platform-routes]. Key endpoints include:

- `/billing/current_spend`: Returns current usage against monthly limits with reset timestamps
- `/billing/rate_limits`: Returns configured rate limits for the organization
- `/billing/prepaid/credits`: Returns credit balance and auto-reload settings
- `/billing/api_keys/usage`: Returns API key usage data over a configurable cutoff period
- `/usage_activities`: Returns usage activity data with configurable granularity
- `/usage_cost`: Returns cost breakdowns by category

The current implementation returns placeholder or constant values for many billing endpoints, providing a skeleton for integration with billing backends.

## Organization Management

Organization routes support CRUD operations for platform organizations [@platform-routes]:

- `GET /`: Fetch organization details including name, settings, and default workspace settings
- `PUT /`: Update organization name, settings, or default workspace settings

Organization updates support patching individual fields like `name`, `settings`, and `default_workspace_settings.enable_api_keys` without requiring the full object to be sent [@platform-routes].

## Analytics

Analytics routes provide session and usage data for console dashboards [@platform-routes]:

- `/analytics/sessions/overview`: Returns aggregate session metrics including error rates, token counts, duration percentiles, and stop reason distribution
- `/analytics/sessions/timeseries`: Returns timeseries data for sessions with configurable grouping

Analytics responses include metric buckets with total, p50, p90, and p95 values where applicable.

## Console Routes

Console-specific routes support web interface features:

- `/console_onboarding/tasks`: Returns onboarding task checklist and panel state
- `/workspaces`: Lists workspaces within the current organization context
- `/admin_requests/join_org`: Lists pending organization join requests for admin review

These routes are scoped to console UI interactions rather than API client usage.

## Authentication and Authorization

Platform API routes use the same authentication middleware as the service API, extracting principals from the request context. However, platform routes are designed for console UI consumption and may use different authorization checks focused on organization membership and seat tiers rather than API key permissions.

The bootstrap endpoint specifically handles cases where no user is authenticated by returning a limited compatibility response, allowing the console to initialize even for unauthenticated visitors.

## Response Conventions

Platform API responses use JSON throughout but do not follow Anthropic API compatibility constraints. Errors may be returned as simple JSON objects with an `error` field rather than the structured error format used by `/v1/*` endpoints.

Constants like default credit balance (100) and monthly limit (50,000) are currently hardcoded, representing platform defaults rather than dynamic configuration [@platform-routes].
