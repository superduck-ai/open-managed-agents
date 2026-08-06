# Open Managed Agents

Ubiquitous language for product and domain concepts used across this repository.

## Vault secrets

**Vault**:
A workspace-scoped collection of credentials that can be attached to a working session so the session may use those credentials.
_Avoid_: treating Vault as a single secret string; conflating Vault with Credential

**Credential**:
One authenticatable identity inside a Vault: non-secret attributes plus, when required, secret material.
_Avoid_: calling the whole Vault a credential; using “token” for the entire credential record

**Secret**:
Only the secret material of a credential. At rest it exists as a Secret envelope; in process memory it may exist briefly as a Transient secret payload.
_Avoid_: calling non-secret auth metadata a secret; equating Secret with Vault

**Secret envelope**:
The at-rest form of a vault credential secret: ciphertext, nonce, wrapped DEK, and key/format metadata. This is what PostgreSQL stores.
_Avoid_: sealed blob (when used alone), encrypted payload column name soup

**Transient secret payload**:
The in-memory plaintext JSON of a credential secret, assembled only for seal/open, merge, validate, or future runtime use. It must never be persisted once envelope storage is the sole at-rest form.
_Avoid_: treating API/DB `secret_payload` as a long-lived stored field; “plaintext column”

**Archived credential**:
A vault credential that remains as metadata only: its secret envelope must be cleared so no secret material remains at rest after archive.
_Avoid_: soft-delete that leaves ciphertext; “archived but still decryptable”

**Active credential**:
A non-archived vault credential that must carry a complete secret envelope whenever it has an auth type that requires a secret (current product types).
_Avoid_: active row with null envelope as a normal state

**Credential secret update (preserve-on-omit)**:
When an update request omits a new secret, the existing secret envelope is opened, merged with the request’s non-secret fields, and resealed under a fresh DEK.
_Avoid_: leaving the old envelope bytes untouched across auth merges; requiring clients to resubmit tokens on every metadata edit

**Direct envelope cutover**:
Replace plaintext-at-rest with secret envelopes in one schema step (add envelope columns, drop plaintext storage) without an Expand/Backfill/Contract dual-read window. Pre-existing plaintext rows are not migrated; their secret material is discarded with the dropped column. No backfill API remains.
_Avoid_: Expand/Backfill dual-write era (for pre-launch); “migration phases” when no production plaintext exists; one-shot migrate-time re-encrypt of legacy rows; `backfill_secrets` maintenance routes

**Missing envelope on active credential**:
An active credential without a secret envelope is treated as a client-recoverable “secret missing” condition (re-submit the secret), not as silent success and not primarily as opaque storage corruption.
_Avoid_: passthrough with no secret; treating omit-secret updates as successful when no envelope exists to preserve

**Envelope open failure**:
When a secret envelope is present but cannot be opened (tamper, wrong KEK, AAD mismatch), the operation fails closed as a server-side fault. This is distinct from a missing envelope on an active credential.
_Avoid_: mapping decrypt failures to “please resubmit token”; treating tamper as not-found

**Envelope integrity enforcement**:
Completeness of a secret envelope and the active/archived lifecycle rules are enforced in application write paths, not by PostgreSQL CHECK constraints.
_Avoid_: relying on DB CHECKs for archive-vs-active envelope presence

## Vault runtime injection

**Runtime credential injection**:
The control-plane act of attaching a credential’s secret to a Managed Agent MCP upstream request inside the session MCP HTTP proxy (`/v2/ccr-sessions/{id}/mcp`), so the sandbox never holds the real token. The matched URL is the real `mcp_url` target, not the proxy URL itself.
_Avoid_: putting tokens in `mcp_config`; “injecting into the agent prompt”; conflating with Environment networking allowlists; treating upstream CONNECT MITM as the MCP injection hop

**Vault attachment (`vault_ids`)**:
The ordered list of Vaults bound to a Session (or Deployment). MCP HTTP proxy injection walks credentials from these vaults in list order when forwarding a real `mcp_url`.
_Avoid_: treating attachment as embedding secrets in the sandbox; unordered sets when match order matters; using attachment to force upstream CONNECT MITM

**Injectable credential (MVP)**:
Only `static_bearer` participates in Runtime credential injection. Other credential auth types may exist in a Vault but are skipped by the injector.
_Avoid_: treating `mcp_oauth` or `environment_variable` as injectable in this slice; requiring every credential in an attached Vault to be injectable

**Credential URL match**:
Selecting a credential when the real `mcp_url` host equals the credential `mcp_server_url` host and the credential path is a `/`-segment prefix of the request path.
_Avoid_: substring path matching that would make `/mcp` hit `/mcp-admin`; host-only matching without path rules; matching against the proxy URL instead of `mcp_url`

**Inject-and-forward**:
On a successful match for an injectable credential type inside the MCP HTTP proxy, strip any client `Authorization`, set `Authorization: Bearer <token>` from a Transient secret payload, and forward to the real `mcp_url`. Non-matches may passthrough or fail closed per host/path rules.
_Avoid_: following cross-origin redirects while holding the injected Authorization; falling through to another credential after Open fails; injecting on the upstream CONNECT MITM path

**Injection passthrough**:
When the real `mcp_url` host is not covered by any attached credential’s `mcp_server_url`, the MCP HTTP proxy forwards without changing Authorization (including when the Session has non-empty `vault_ids`).
_Avoid_: requiring every MCP host to have a vault credential; treating passthrough as “injection succeeded”; using attachment alone as a global “must authenticate all MCP” switch

**Injection fail-closed**:
Reject the MCP proxy upstream request when a same-host credential path does not match, or when opening a matched credential’s envelope fails. The proxy surfaces these as upstream credential unavailability (HTTP 502), not as a client auth failure on the session JWT.
_Avoid_: silent passthrough without token after a match; trying the next vault credential after Open failure; failing closed on MITM disable for MCP injection (MCP proxy does not require MITM)
