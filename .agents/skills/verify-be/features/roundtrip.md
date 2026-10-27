# Send and receive a chat message

## Sub-features

Input acceptance, JetStream delivery/ACK, real Worker execution, model proxy lifecycle, Core NATS preview fanout and final response persistence.

## How to get to it (user POV)

Open an existing chat and send a message. API equivalents are `POST /v1/sessions/{id}/events?beta=true` and `GET /v1/sessions/{id}/events/stream?beta=true&event_deltas[]=agent.message`, authenticated with the workspace API key.

## Driving it with verify-be

Run `.agents/skills/verify-be/scripts/verify-be chat roundtrip`.
`tests/liveworker/chat_roundtrip_test.go` waits for SSE readiness, sends one input, observes a Chinese preview before permitting the model to finish, compares preview/final IDs and text, checks model request start/end history, then waits for idle and an empty input queue. The report records each completed phase.

## Gotchas

Session activation is a fixture, not public Runner provisioning. The upstream model is scripted, but the Worker and model proxy are real. The test sends one message and does not assert concurrent ordering. Successful input submission alone does not mean execution completed. Preview delivery is not a durable token stream.
