### Product & Experimentation (2 criteria)

**Application Scope:**
- **product_analytics_instrumentation** (Level 3, App): Product analytics instrumentation – Mixpanel, Amplitude, PostHog, Heap, or GA4 is instrumented in the application. Agents can see whether features are actually used and measure the impact of their changes on user behavior.
- **error_to_insight_pipeline** (Level 5, App): Error to insight pipeline – Check for Sentry-GitHub/GitLab integration: search for sentry.io webhook in .github/workflows or repo settings, OR Sentry issue linking config (SENTRY_ORG, SENTRY_PROJECT in env). Also check for error-to-issue automation: GitHub Actions that create issues from errors, or PagerDuty/OpsGenie integrations with issue creation. PASS if any error tracking tool has issue creation integration configured.
