// Protocol structures copied from superduck-code/src/cli/transports/ccrClient.ts
// and src/utils/sessionState.ts. Runtime code remains local to this plugin.

export type EventPayload = {
  uuid: string
  type: string
  [key: string]: unknown
}

export type ClientEvent = {
  payload: EventPayload
  ephemeral?: boolean
}

// Delivery uploader item from superduck-code CCRClient.
export type DeliveryUpdate = {
  eventId: string
  status: 'received' | 'processing' | 'processed'
}

export type WorkerEvent = {
  payload: EventPayload
  is_compaction?: boolean
  agent_id?: string
}

export type InternalEvent = {
  event_id: string
  event_type: string
  payload: Record<string, unknown>
  event_metadata?: Record<string, unknown> | null
  is_compaction: boolean
  created_at: string
  agent_id?: string
}

export type ListInternalEventsResponse = {
  data: InternalEvent[]
  next_cursor?: string | null
}

export type WorkerStateResponse = {
  worker?: {
    external_metadata?: Record<string, unknown>
  }
}

// Return shape from superduck-code CCRClient.getWorkerState.
export type WorkerStateReadResult = {
  metadata: Record<string, unknown> | null
  durationMs: number
}

export type SessionState = 'idle' | 'running' | 'requires_action'

/**
 * Context carried with requires_action transitions so downstream
 * surfaces (CCR sidebar, push notifications) can show what the
 * session is blocked on, not just that it's blocked.
 *
 * Two delivery paths:
 * - tool_name + action_description → RequiresActionDetails proto
 *   (webhook payload, typed, logged in Datadog)
 * - full object → external_metadata.pending_action (queryable JSON
 *   on the Session, lets the frontend iterate on shape without
 *   proto round-trips)
 */
export type RequiresActionDetails = {
  session_thread_id?: string
  pending_actions?: RequiresActionDetails[]
  tool_name: string
  /** Human-readable summary, e.g. "Editing src/foo.ts", "Running npm test" */
  action_description: string
  tool_use_id: string
  request_id: string
  /** Raw tool input — the frontend reads from external_metadata.pending_action.input
   * to parse question options / plan content without scanning the event stream. */
  input?: Record<string, unknown>
}

export type SessionExternalMetadata = {
  permission_mode?: string | null
  model?: string | null
  pending_action?: RequiresActionDetails | null
  // Opaque — typed at the emit site. Importing PostTurnSummaryOutput here
  // would leak the import path string into sdk.d.ts via agentSdkBridge's
  // re-export of SessionState.
  post_turn_summary?: unknown
  // Mid-turn progress line from the forked-agent summarizer — fires every
  // ~5 steps / 2min so long-running turns still surface "what's happening
  // right now" before post_turn_summary arrives.
  task_summary?: string | null
}
