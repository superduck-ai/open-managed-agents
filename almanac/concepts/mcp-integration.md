---
title: "MCP Integration"
summary: "Model Context Protocol server integration for secure credential management and OAuth flow handling."
topics: [integration, security, oauth]
sources:
  - id: vaults-handler
    type: file
    path: internal/vaults/handler.go
  - id: platform-mcp-oauth
    type: file
    path: internal/api/platform_mcp_vault_auth.go
  - id: mcp-oauth-flows
    type: file
    path: internal/db/mcp_oauth_flows.go
  - id: api-server
    type: file
    path: internal/api/server.go
---

Model Context Protocol (MCP) integration provides secure credential management for external MCP servers through vaults and OAuth flows. The system supports multiple credential types including OAuth-based authentication and static bearer tokens.

## Credential Types

MCP vaults support three credential auth types:

- **`mcp_oauth`**: OAuth 2.0 flow with PKCE, supporting dynamic client registration and token refresh
- **`static_bearer`**: Static bearer token authentication for MCP servers
- **`environment_variable`**: Environment variable injection with optional networking restrictions [@vaults-handler]

OAuth credentials store access tokens separately from public metadata, with refresh tokens maintaining endpoint configuration for automatic renewal. The vault system enforces a maximum of 20 active credentials per vault.

## OAuth Flow

The MCP OAuth flow follows a multi-step authorization process:

1. **Discovery**: The system fetches OAuth metadata from protected resource endpoints (`.well-known/oauth-protected-resource`) and authorization server metadata (`.well-known/oauth-authorization-server` or `.well-known/openid-configuration`) [@platform-mcp-oauth]

2. **Client Registration**: If no client credentials are provided, the system dynamically registers an OAuth client using the registration endpoint, requesting `authorization_code` and `refresh_token` grant types

3. **PKCE Generation**: A code verifier and challenge are generated using SHA256 (or plain if the server requires it)

4. **Authorization Flow**: The user is redirected to the authorization endpoint with `code_challenge` and `state` parameters

5. **Token Exchange**: The callback exchanges the authorization code for an access token (and optionally refresh token) using the `code_verifier`

6. **Credential Creation**: Upon successful token exchange, a vault credential is created with the access token stored in the encrypted `secret_payload` and public refresh configuration stored in `auth`

The entire OAuth flow has a 15-minute TTL and is tracked in the `mcp_oauth_flows` table with `pending`, `completed`, or `failed` status [@mcp-oauth-flows].

## Vault API

MCP vaults are managed through the `/v1/vaults` API endpoint (requires `beta=true`):

- `POST /v1/vaults` - Create a new vault
- `GET /v1/vaults` - List vaults with cursor-based pagination
- `POST /v1/vaults/{vault_id}/credentials` - Create a credential in a vault
- `GET /v1/vaults/{vault_id}/credentials` - List credentials in a vault
- `POST /v1/vaults/{vault_id}/credentials/{credential_id}/mcp_oauth_validate` - Validate MCP OAuth credentials [@vaults-handler]

All vault and credential operations emit webhooks (`vault.created`, `vault.archived`, `vault_credential.created`, etc.) for downstream consumers.

## Platform Integration

The platform console provides MCP vault OAuth authorization through:

- `POST /api/organizations/{orgUuid}/mcp/vault-auth/start` - Initiates OAuth flow for a vault
- `GET /oauth/vault/success` - OAuth callback handler that completes the flow [@api-server]

The callback handler delivers the OAuth result via `BroadcastChannel` and `window.opener.postMessage`, then automatically closes the popup window.
