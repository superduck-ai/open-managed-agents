# Recover final chat history

## Sub-features

Read persisted input and final response after closing the live stream. Match event identity, thread, text and input acceptance. Detect duplicate final Worker echoes.

## How to get to it (user POV)

Reopen the same conversation. The backend equivalent is `GET /v1/sessions/{id}/events?beta=true&order=asc&limit=100` with the same workspace identity.

## Driving it with verify-chat

Run `chat.roundtrip`. It closes public SSE after receiving the final answer and reads both session history and `/threads/{thread_id}/events`, then repeats after the Worker becomes idle. Exactly one input, one answer and one model start/end pair must remain in each view; ephemeral preview records must not appear in history.

## Gotchas

This proves historical readback after a successful turn. It does not inject a mid-stream disconnect, restart PostgreSQL or test archive restoration. Session SSE has no automatic `Last-Event-ID` replay; recovery uses history. Public `processed_at` records server acceptance; Worker ACK does not change it or reorder public history.

Primary session history can omit `session_thread_id`. The thread-specific endpoint supplies it. The gate permits an implicit primary thread in session history, requires an explicit matching thread in thread history, and rejects any conflicting thread ID.
