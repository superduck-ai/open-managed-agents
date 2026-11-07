---
name: oma
description: Use the official Claude API skill workflow with Open Managed Agents through the compatible Anthropic API and ant CLI. Configure the target OMA URL and API key, onboard agents, and integrate sessions.
---

# Building applications with Open Managed Agents

Use the bundled official Claude API skill and its original task routing, interview, resource definitions, and setup/runtime split. Apply only the deployment adaptations below. Respond in the user's language; do not impose a language.

## Before you start

Read [connection configuration](references/connection.md). Resolve the URL from the user's explicit target or `OMA_BASE_URL`; ask when neither is provided. Resolve a key belonging to that target, then map both to the standard Anthropic environment variables. There is no hard-coded deployment URL.

The endpoint is connection configuration, not the task-source URL accepted by the upstream onboarding command. Treat a URL following `managed-agents-onboard` as task material unless the user explicitly identifies it as the API endpoint.

## Upstream workflow

Read [the original skill](upstream/claude-api/SKILL.md) and follow its matching subcommand and language routing. For `onboard` or `managed-agents-onboard`, execute [Managed Agents onboarding](upstream/claude-api/shared/managed-agents-onboarding.md), including its task URL and quickstart-template branches. Resolve upstream `shared/` and language paths relative to `upstream/claude-api/`.

Keep upstream behavior: propose agent configuration, reuse or configure an environment, collect required integration credentials, smoke-test a session, then deliver version-controlled setup with ant apply and application runtime with the official SDK. Do not replace this with a separate OMA interview or custom CLI.

## Deployment adaptations

- Send CLI and SDK requests to the configured OMA origin with its API key. Replace official Console login and cloud-provider authentication with OMA credentials. Set the SDK's base URL explicitly where environment configuration is not supported. Do not emit Anthropic Console URLs as OMA links.
- OMA can expose multiple model providers behind the compatible API. Upstream provider-detection rules must not redirect an OMA task to a different platform. Choose an available model appropriate for the task; upstream model defaults and pricing are not OMA deployment facts.
- Preserve upstream SDK/API syntax for supported functionality. API compatibility is the default integration path; it does not establish that every optional feature or model setting is configured in every deployment. Confirm task-critical capabilities using that instance and its actual responses.
- Read [observed compatibility notes](references/cli.md) when operating environments or consuming events. They describe the tested instance, not universal OMA restrictions. Prefer the original commands when supported; use the documented fallback for a confirmed mismatch.
- Keep setup separate from runtime, preserve resource IDs and lockfiles, and associate completion with the current input event and subsequent model request rather than an earlier turn's idle event.

See [validation scope](references/validation.md) for tested behavior and [upstream provenance](upstream/SOURCE.md) for the pinned source and license. The upstream files retain their original content apart from whitespace normalization.
