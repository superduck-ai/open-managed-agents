# Chat reliability

Run `just verify-be chat reliability`.

The first real Worker model request is held after a preview. A second public message must return HTTP 409 with `conflict_error`, add no input to history and start no model request. The test drops the Worker's SSE connection, observes a new connection and checks that the model request was not duplicated. It closes the public client stream mid-turn, releases the model and recovers the completed turn from history. Once idle, retrying the rejected message must succeed. Both turns must remain in order, with no duplicate IDs or persisted preview events.

It then destroys the Worker container, rotates the production Code Session credentials, verifies that the old credential is rejected, registers the replacement epoch and submits another message before starting the replacement real Worker. The third turn must complete once, with correct history, idle status and a drained queue.

The test drives the production recovery service directly. Automatic detection of a crashed cloud sandbox, cloud replacement allocation, process-memory restoration and replay after a crash in the middle of a tool side effect are not certified by this scenario. Do not inject faults into shared developer services.

Run `just verify-be chat instances` for two independently started backend processes sharing this run's PostgreSQL, Redis and NATS. Input and Worker requests go to the first process; public SSE and history requests go to the second. Both preview and final response must arrive with matching content and IDs. The peer process is terminated during cleanup.

After interruption, the public client subscribes again before issuing the next turn and scanning history in pages of two. The interrupted preview must have the same ID as the complete history final. The new stream must deliver the next final, and the full history must contain each final once with ordered inputs. This verifies backend resubscription and full-history reconciliation inputs; it does not implement or certify browser merge logic. There is no Last-Event-ID replay contract. Existing frontend tests cover its merge and reconnect behavior separately.
