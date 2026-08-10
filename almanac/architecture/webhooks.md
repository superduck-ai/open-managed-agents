---
title: "Webhooks"
summary: "Webhook endpoint configuration, event delivery system, and retry handling for real-time notifications of workspace events."
topics: [architecture]
sources:
  - id: webhooks-handler
    type: file
    path: internal/webhooks/handler.go
  - id: webhooks-db
    type: file
    path: internal/db/webhooks.go
  - id: webhooks-package
    type: file
    path: internal/webhooks/webhooks.go
---

Webhooks provide real-time notifications for events occurring within a workspace. Clients configure webhook endpoints specifying which event types they want to receive, and the system delivers JSON payloads to those URLs as events occur. Webhook delivery is asynchronous with retry logic for failed deliveries.

## Webhook Endpoints

A webhook endpoint represents a client's integration configuration:

- **External ID**: Unique identifier like `wh_abc123`
- **URL**: The HTTPS endpoint where events will be delivered
- **Name**: Human-readable identifier for the webhook
- **Description**: Optional detailed description
- **Enabled events**: List of event types this webhook subscribes to
- **Status**: `enabled` or `disabled`
- **Signing secret**: Cryptographic key for verifying webhook signatures
- **Disabled reason**: Reason for being disabled (if applicable) [@webhooks-handler]

Webhooks are scoped to a specific workspace and are created by workspace API keys. Once configured, they receive events for that workspace only.

## Event Types

The system supports various endpoint event types across different resources:

**Session events:**
- `session.status_run_started`, `session.status_idled`, `session.status_rescheduled`, `session.status_terminated`
- `session.deleted`, `session.updated`, `session.error`
- `session.thread_created`, `session.thread_status_running`, `session.thread_status_idle`, `session.thread_status_rescheduled`, `session.thread_status_terminated`
- `session.thread_idled`, `session.thread_terminated`
- `session.outcome_evaluation_ended`

**Vault events:**
- `vault.created`, `vault.archived`, `vault.deleted`
- `vault_credential.created`, `vault_credential.archived`, `vault_credential.deleted`
- `vault_credential.refresh_failed` [@webhooks-handler]

Clients specify which events they want to receive in the `enabled_events` array when creating or updating a webhook.

## Webhook Configuration

Creating a webhook endpoint requires:

- **URL**: Valid HTTPS URL (optionally HTTP if `WEBHOOK_ALLOW_INSECURE=true`)
- **Name**: Non-empty string up to 255 characters
- **Enabled events**: At least one event type from the supported set

URL validation enforces several security requirements:

- Must use HTTPS scheme unless insecure mode is enabled
- Cannot include embedded credentials (username/password)
- Must use port 443 for HTTPS unless insecure mode is enabled
- Must be a publicly routable hostname (localhost and private IPs rejected unless insecure mode enabled) [@webhooks-handler]

## Signing Secrets

Each webhook is generated with a unique signing secret during creation. The signing secret:

- **Format**: `whsec_` prefix followed by base64-encoded 32 random bytes
- **Purpose**: Allows webhook recipients to verify event authenticity
- **Return**: Only returned in the response body immediately after creation
- **Storage**: Stored securely and used to sign all outgoing webhook deliveries
- **Rotation**: Can be regenerated via the `/regenerate_signing_secret` endpoint [@webhooks-handler]

Clients should store the signing secret securely and use it to compute HMAC signatures for incoming webhook deliveries to verify they originated from the system.

## Event Delivery

When an event occurs, the system enqueues an asynchronous job for delivery to matching webhooks. The delivery process:

1. **Event matching**: Finds webhooks subscribed to the event type in `enabled` status
2. **Job creation**: Enqueues delivery jobs with event payload and endpoint information
3. **Lease acquisition**: Worker leases jobs for exclusive processing
4. **HTTP delivery**: Sends POST request to webhook URL with signed payload
5. **Response handling**: Marks job complete on success, schedules retry on failure
6. **Retry escalation**: Uses exponential backoff for failed deliveries with max attempts limit [@webhooks-db]

## Webhook Payloads

Webhook deliveries contain:

- **Event type**: The type of event that occurred
- **Event data**: Resource-specific payload describing the event
- **Timestamp**: When the event occurred
- **Signature**: HMAC-SHA256 signature using the webhook's signing secret

The signature is computed as `HMAC-SHA256(signing_secret, timestamp + "." + body)` and delivered in the `X-Webhook-Signature` header. Recipients can verify authenticity by recomputing this HMAC [@webhooks-package].

## Delivery States

Webhook delivery jobs progress through these states:

- **pending**: Waiting to be attempted
- **running**: Currently being delivered (with lease lock)
- **retry**: Scheduled for retry after previous failure
- **completed**: Successfully delivered
- **failed**: Exhausted retry attempts without success

Workers use database-level locking with timeouts to ensure at-least-once delivery semantics. If a worker crashes during delivery, the lease expires and another worker can claim the job [@webhooks-db].

## Failure Handling

Failed webhook deliveries are retried with:

- **Exponential backoff**: Increasing delays between retry attempts
- **Max attempts**: Configurable limit on retry attempts
- **Error tracking**: Last error stored in job payload
- **State persistence**: Job state persisted across worker restarts [@webhooks-db]

Transient failures (network issues, server errors) are retried. Permanent failures (4xx client errors, invalid URLs) may reach max attempts more quickly.

## Disabled Webhooks

Webhooks can be disabled in two ways:

- **Manual**: Client explicitly sets status to `disabled`
- **Automatic**: System disables after consecutive failures (if configured)

Disabled webhooks are stored but don't receive new events. They can be re-enabled by updating status back to `enabled`. When re-enabling after automatic disable, the `disabled_reason` is cleared and consecutive failure counter resets [@webhooks-handler].

## API Endpoints

The webhooks API requires `anthropic-beta: webhooks-2026-03-01` header and provides:

- `POST /webhooks`: Create a new webhook endpoint
- `GET /webhooks`: List all webhooks in the workspace
- `GET /webhooks/{webhook_id}`: Retrieve a specific webhook
- `POST /webhooks/{webhook_id}`: Update a webhook configuration
- `DELETE /webhooks/{webhook_id}`: Delete a webhook endpoint
- `POST /webhooks/{webhook_id}/regenerate_signing_secret`: Generate a new signing secret

All endpoints return webhook configuration in API responses, with the signing secret only included immediately after creation or regeneration [@webhooks-handler].
