---
title: "Database Schema"
summary: "PostgreSQL schema reference for Open Managed Agents, including tables for organizations, workspaces, agents, sessions, and supporting resources."
topics: [database, reference, architecture]
sources:
  - id: init-migration
    type: file
    path: internal/db/migrations/00001_init.sql
---

The Open Managed Agents database schema uses PostgreSQL 17 with `pgcrypto` for UUID generation. Tables follow naming conventions with `external_id` as the public identifier, `uuid` for internal identity, and standard timestamps including soft delete support.

## Core Entities

### Organizations

The `organizations` table stores top-level organizational units[@init-migration]:

- `id` — Primary key (bigint identity)
- `uuid` — Internal unique identifier (gen_random_uuid)
- `external_id` — Public-facing organization identifier
- `name` — Organization display name
- `settings` — JSONB configuration (default workspace settings)
- `profile` — JSONB profile data

### Workspaces

The `workspaces` table contains workspace entities belonging to organizations[@init-migration]:

- `id`, `uuid`, `external_id` — Identity fields
- `organization_id` — Parent organization reference
- `name` — Workspace name (unique per organization)
- `archived_at` — Soft deletion timestamp
- `compartment_id` — Isolation boundary identifier
- `display_color` — UI presentation color
- `data_residency` — Geographic and inference policies (JSONB)
- `external_key_id` — Associated external key reference
- `tags` — Classification metadata (JSONB)

The unique constraint `(organization_id, name)` prevents duplicate workspace names within an organization.

### API Keys

The `api_keys` table manages credentials for workspace access[@init-migration]:

- `id`, `uuid`, `external_id` — Identity fields
- `workspace_id` — Owning workspace reference
- `key_hash` — Hashed API key value (unique)
- `status` — `active` or archived
- `created_by_user_id` — Creator reference
- `name` — Key display name
- `partial_key_hint` — Key prefix for identification
- `expires_at` — Optional expiration timestamp

The `key_hash` unique constraint prevents key reuse.

### Console API Keys

The `console_api_keys` table extends API key management for console operations[@init-migration]:

- Additional fields: `org_uuid`, `api_key_uuid`, `key_prefix`, `key_suffix`
- `last_used_at` — Activity tracking
- Indexes for organization/workspace scoping and archived filtering

## User and Access Control

### Users

The `users` table stores user accounts with organization-scoped email uniqueness[@init-migration]:

- `role` — One of: `user`, `developer`, `billing`, `admin`, `claude_code_user`
- `deleted_at` — Soft deletion
- Unique index on `(organization_id, lower(email))` where `deleted_at is null`

### Organization Invites

The `organization_invites` table tracks pending invitations[@init-migration]:

- `role` — Same enum as users table
- `status` — `accepted`, `expired`, `deleted`, or `pending`
- `expires_at` — Invitation validity window

### Workspace Members

The `workspace_members` table defines workspace-level access[@init-migration]:

- `workspace_role` — One of: `workspace_user`, `workspace_developer`, `workspace_restricted_developer`, `workspace_admin`, `workspace_billing`
- Unique constraint on `(workspace_id, user_id)` where `deleted_at is null`

## Agent and Deployment

### Agents

The `agents` table stores managed agent configurations[@init-migration]:

- `current_version` — Latest version number
- `name`, `description`, `system` — Agent definition
- `model` — Model configuration (JSONB)
- `mcp_servers` — MCP server configurations (JSONB array)
- `skills` — Attached skill references (JSONB array)
- `tools` — Tool configurations (JSONB array)
- `multiagent` — Subagent configuration (JSONB)
- `metadata` — Custom properties (JSONB)
- `archived_at`, `deleted_at` — Soft deletion timestamps

The unique constraint `(workspace_id, external_id)` prevents external ID collisions within workspaces.

### Agent Versions

The `agent_versions` table stores historical agent configurations[@init-migration]:

- `version` — Version number (unique per agent)
- Snapshot fields mirroring the agent table
- Unique constraint on `(agent_id, version)` and `(workspace_id, agent_external_id, version)`

### Sessions

The `sessions` table tracks active agent execution contexts[@init-migration]:

- `environment_id`, `environment_external_id` — Execution environment reference
- `agent_id`, `agent_external_id`, `agent_version` — Agent snapshot
- `agent_snapshot` — Complete agent configuration at launch (JSONB)
- `deployment_id` — Optional deployment association
- `title` — Session display name
- `status` — One of: `rescheduling`, `running`, `idle`, `terminated`
- `usage`, `stats`, `outcome_evaluations` — Execution metrics (JSONB)
- `vault_ids` — Vault credential references (JSONB array)

### Deployments

The `deployments` table stores scheduled agent configurations[@init-migration]:

- `environment_id`, `agent_id`, `agent_version` — Target configuration
- `agent_snapshot` — Agent configuration (JSONB)
- `initial_events` — Startup event sequence (JSONB array)
- `resources` — Resource attachments (JSONB array)
- `resource_secrets` — Secret configurations (JSONB)
- `vault_ids` — Vault references (JSONB array)
- `schedule` — Scheduling policy (JSONB)
- `status` — `active` or `paused`
- `paused_reason` — Status explanation (JSONB)

### Deployment Runs

The `deployment_runs` table tracks deployment execution history[@init-migration]:

- `deployment_id`, `deployment_external_id` — Parent deployment
- `agent_snapshot` — Agent configuration at run time (JSONB)
- `session_external_id` — Resulting session reference
- `error` — Failure details (JSONB)
- `trigger_type` — `manual` or `schedule`
- `trigger_context` — Trigger metadata (JSONB)

## Environments and Sandboxes

### Environments

The `environments` table defines execution environments[@init-migration]:

- `name` — Environment name (unique per workspace where active)
- `scope` — `organization` or `account` level visibility
- `provider` — Currently only `e2b`
- `resolved_template` — Sandbox template identifier
- `config` — Provider configuration (JSONB)

### Environment Work

The `environment_work` table tracks sandbox execution jobs[@init-migration]:

- `state` — One of: `queued`, `starting`, `active`, `stopping`, `stopped`
- `claimed_by_worker_id` — Worker lease holder
- `claim_expires_at` — Lease expiration
- `acknowledged_at`, `started_at` — Lifecycle timestamps
- `latest_heartbeat_at` — Worker activity signal
- `heartbeat_ttl_seconds` — Lease duration
- `stop_requested_at`, `stopped_at` — Shutdown lifecycle

### Environment Sandboxes

The `environment_sandboxes` table tracks provider sandbox instances[@init-migration]:

- `work_id`, `work_external_id` — Parent work reference
- `provider` — Currently only `e2b`
- `template` — Sandbox template identifier
- `provider_sandbox_id` — Provider's sandbox identifier
- `state` — One of: `creating`, `running`, `stopping`, `stopped`, `failed`
- `last_error` — Failure description

## Files and Skills

### Files

The `files` table manages uploaded file storage[@init-migration]:

- `workspace_id` — Owning workspace
- `filename`, `mime_type`, `size_bytes` — File metadata
- `sha256` — Content hash
- `s3_bucket`, `s3_key` — Object storage location
- `downloadable` — Download permission flag
- `scope_type`, `scope_id` — Optional resource scoping

### Skills

The `skills` table stores workspace skill definitions[@init-migration]:

- `display_title` — Human-readable name
- `latest_version` — Current version reference
- `source` — Currently only `custom`
- Unique constraint on `(workspace_id, external_id)`

### Skill Versions

The `skill_versions` table stores skill archives[@init-migration]:

- `skill_id`, `skill_external_id` — Parent skill reference
- `version` — Version identifier (unique per skill)
- `name`, `description` — Skill metadata
- `directory` — Top-level directory name
- `s3_bucket`, `s3_key` — Archive storage location
- `size_bytes`, `sha256` — Archive metadata

### Builtin Skills

The `builtin_skills` and `builtin_skill_versions` tables manage platform-provided skills (not in the init migration but added subsequently). These tables mirror the custom skill structure with `source: "anthropic"`.

## Memory Stores

### Memory Stores

The `memory_stores` table defines persistent memory containers[@init-migration]:

- `name` — Store name (1-255 characters)
- `description` — Store documentation (up to 1024 characters)

### Memories

The `memories` table stores individual memory entries[@init-migration]:

- `memory_store_id`, `memory_store_external_id` — Parent store reference
- `current_version_id`, `current_version_external_id` — Latest version reference
- `path` — Entry path identifier
- `content_size_bytes`, `content_sha256` — Content metadata
- `s3_bucket`, `s3_key` — Content storage location
- Unique constraint on `(memory_store_id, path)` where `deleted_at is null`

### Memory Versions

The `memory_versions` table tracks memory entry history[@init-migration]:

- `memory_id`, `memory_external_id` — Parent memory reference
- `operation` — One of: `created`, `modified`, `deleted`
- `path` — Entry path (null for deleted operations)
- `content_size_bytes`, `content_sha256`, `s3_bucket`, `s3_key` — Content data (null for deleted)
- `created_by_actor_type` — Originator: `api_actor`, `session_actor`, or `user_actor`
- `created_by_api_key_id`, `created_by_session_id`, `created_by_user_id` — Actor references
- `redacted_at` — Redaction timestamp

## Vaults and Credentials

### Vaults

The `vaults` table stores credential containers[@init-migration]:

- `display_name` — Vault name (1-255 characters)
- `metadata` — Custom properties (JSONB)

### Vault Credentials

The `vault_credentials` table manages stored secrets[@init-migration]:

- `vault_id`, `vault_external_id` — Parent vault reference
- `display_name` — Credential name (1-255 characters)
- `auth_type` — One of: `mcp_oauth`, `static_bearer`, `environment_variable`
- `credential_key` — Credential identifier
- `auth` — Authentication configuration (JSONB)
- `secret_payload` — Encrypted secret data (JSONB)
- Unique constraint on `(vault_id, credential_key)` where `deleted_at is null` and `archived_at is null`

### MCP Tunnels

The `mcp_tunnels` table stores MCP tunnel configurations[@init-migration]:

- `domain` — Tunnel domain (unique)
- `token_id`, `tunnel_token` — Tunnel authentication

### MCP Tunnel Certificates

The `mcp_tunnel_certificates` table stores tunnel TLS certificates[@init-migration]:

- `tunnel_id`, `tunnel_external_id` — Parent tunnel reference
- `ca_certificate_pem` — Certificate authority certificate
- `fingerprint` — Certificate fingerprint
- `expires_at` — Certificate expiration

## Session Resources and Events

### Session Resources

The `session_resources` table tracks resource attachments to sessions[@init-migration]:

- `resource_type` — One of: `file`, `github_repository`, `memory_store`
- `payload` — Resource configuration (JSONB)
- `secret_payload` — Sensitive data (JSONB)

### Session Events

The `session_events` table records session event history[@init-migration]:

- `session_id`, `session_external_id` — Parent session reference
- `thread_id`, `thread_external_id` — Optional thread association
- `event_type` — Event discriminator
- `payload` — Event data (JSONB)
- `processed_at` — Processing timestamp

### Session Threads

The `session_threads` table tracks conversation threads[@init-migration]:

- `session_id`, `session_external_id` — Parent session reference
- `parent_thread_id`, `parent_thread_external_id` — Thread hierarchy
- `agent_snapshot` — Agent configuration (JSONB)
- `status` — One of: `rescheduling`, `running`, `idle`, `terminated`

## Code Sessions

### Code Sessions

The `code_sessions` table manages Claude Code execution contexts[@init-migration]:

- `environment_id`, `environment_external_id` — Environment reference
- `work_dir` — Working directory path
- `permission_mode` — Permission mode identifier
- `model` — Model identifier
- `connection_status` — One of: `connected`, `disconnected`
- `last_inbound_sequence_num`, `last_outbound_sequence_num` — Event ordering
- `last_worker_connected_at`, `last_worker_activity_at` — Worker activity tracking
- Index on `(workspace_id, session_external_id)` where `deleted_at is null`

### Code Session Events

Inbound and outbound events use separate tables with `sequence_num` ordering and idempotency keys for exactly-once delivery semantics.

## Background Jobs

### Jobs

The `jobs` table manages background job processing[@init-migration]:

- `type` — Job type discriminator
- `status` — Job status
- `payload` — Job data (JSONB)
- `attempts` — Retry counter
- `locked_by`, `locked_until` — Worker lease
- `run_after` — Scheduled execution time

Indexes on `(status, run_after, created_at)` and `(type, status, run_after, created_at)` support efficient polling.

## Webhooks

### Webhook Endpoints

The `webhook_endpoints` table stores webhook configurations[@init-migration]:

- `url` — Endpoint URL
- `name` — Display name
- `enabled_events` — Event filter (JSONB array)
- `signing_secret` — Signature verification key
- `status` — `enabled` or `disabled`
- `disabled_reason` — Status explanation
- `consecutive_failures` — Failure counter

## Message Batches

### Message Batches

The `message_batches` table manages batch API requests[@init-migration]:

- `api_variant` — `stable` or `beta`
- `anthropic_version` — API version string
- `beta_headers` — Beta header configuration (JSONB array)
- `processing_status` — `in_progress`, `canceling`, or `ended`
- `request_count` — Total request count (must be positive)
- `processing_count`, `succeeded_count`, `errored_count`, `canceled_count`, `expired_count` — Result counters
- `results_s3_bucket`, `results_s3_key`, `results_size_bytes`, `results_sha256` — Batch results
- `cancel_initiated_at`, `ended_at`, `last_error` — Cancellation tracking

### Message Batch Requests

The `message_batch_requests` table stores individual batch requests[@init-migration]:

- `message_batch_id` — Parent batch reference
- `request_index` — Request position (unique per batch)
- `custom_id` — User-provided identifier
- `params` — Request parameters (JSONB)
- `status` — `queued`, `in_flight`, `succeeded`, `errored`, `canceled`, or `expired`
- `result` — Response data (JSONB)
- `upstream_request_id` — Upstream API reference
- `started_at`, `completed_at` — Execution timestamps
- `in_flight_worker_id` — Worker handling the request

Unique constraints on `(message_batch_id, custom_id)` and `(message_batch_id, request_index)` ensure request uniqueness.

## External Keys

The `external_keys` table manages external service credentials at the organization level[@init-migration]:

- `display_name` — Key display name
- `geo` — Geographic constraint (currently only `us`)
- `provider_config` — Provider-specific configuration (JSONB)

## Constraints and Indexes

All tables use `id bigint generated always as identity` for primary keys. Foreign key relationships are not enforced at the database level—application logic maintains referential integrity. Soft deletion uses `deleted_at timestamptz` columns with partial unique indexes excluding deleted rows.

Common index patterns include:
- `(workspace_id, created_at desc, id desc)` where `deleted_at is null` for temporal queries
- `(workspace_id, status, created_at desc, id desc)` where `deleted_at is null` for status-filtered lists
- Partial unique indexes for `external_id` uniqueness within active rows
- Composite indexes for common query patterns

The schema uses JSONB for flexible metadata storage (`settings`, `profile`, `metadata`, `config`) while keeping structured fields as columns for efficient querying.
