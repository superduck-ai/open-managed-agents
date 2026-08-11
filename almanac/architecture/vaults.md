---
title: "Vaults"
summary: "Secure credential storage for API keys, tokens, and secrets with support for MCP OAuth flows and workspace-scoped access."
topics: [architecture]
sources:
  - id: vaults-handler
    type: file
    path: internal/vaults/handler.go
---

Vaults provide secure storage for credentials that managed agents need to access external services. Each vault contains multiple credentials, and vaults can be referenced by deployments and sessions to inject credentials into agent execution environments. Vaults support multiple authentication types including OAuth flows for MCP (Model Context Protocol) servers.

## Vault Model

A vault is a workspace-scoped container for credentials:

- **External ID**: Unique identifier like `vlt_abc123`
- **Display name**: Human-readable name (up to 255 characters)
- **Description**: Optional detailed description
- **Metadata**: Key-value pairs for organization (up to 16 entries, keys up to 64 bytes, values up to 512 bytes)
- **Archived at**: Optional timestamp when the vault was archived [@vaults-handler]

Vaults are soft-deleted and support archiving. When a vault is archived, all its credentials are also archived and their secret payloads are cleared.

## Credential Types

Vaults support three credential authentication types:

**MCP OAuth (`mcp_oauth`):**
- Designed for OAuth-based MCP server authentication
- Stores access token with optional expiration
- Supports refresh token flow with token endpoint configuration
- Includes client credentials for token renewal

**Static Bearer (`static_bearer`):**
- Simple bearer token for API authentication
- Stores static token value
- Associated with a specific MCP server URL

**Environment Variable (`environment_variable`):**
- Injects credentials as environment variables in agent sessions
- Stores secret name and value pairs
- Supports networking restrictions (allowed hosts) [@vaults-handler]

Each credential type has different validation rules and storage patterns.

## MCP OAuth Credentials

MCP OAuth credentials provide the most complete authentication flow for OAuth-protected MCP servers:

**Required fields:**
- `mcp_server_url`: HTTPS URL of the MCP server
- `access_token`: Current OAuth access token
- Optional `expires_at`: Token expiration timestamp

**Refresh configuration (optional):**
- `token_endpoint`: OAuth token endpoint URL
- `client_id`: OAuth client identifier
- `refresh_token`: Refresh token for obtaining new access tokens
- `token_endpoint_auth`: Authentication method for token endpoint (`none`, `client_secret_basic`, or `client_secret_post`)
- Optional `scope` and `resource`: OAuth parameters [@vaults-handler]

The refresh configuration enables automatic token renewal when the access token expires. The system stores both the public OAuth configuration and the secret tokens separately for security.

## Static Bearer Credentials

Static bearer credentials provide simple token-based authentication:

- **mcp_server_url**: HTTPS URL of the MCP server
- **token**: Bearer token value

The token is stored in the secret payload and not returned in most API responses. The public configuration includes only the server URL [@vaults-handler].

## Environment Variable Credentials

Environment variable credentials inject secrets into agent execution environments:

- **secret_name**: Environment variable name (up to 255 characters)
- **secret_value`: Secret value to inject
- **networking**: Optional access restrictions

**Networking options:**
- **unrestricted**: No network restrictions (default)
- **limited**: Restrict to specific allowed hosts

For limited networking:
- **allowed_hosts**: List of up to 16 hostnames
- **Hostname format**: Valid DNS names or wildcards like `*.example.com`
- **Max length**: 253 characters per hostname [@vaults-handler]

This enables fine-grained control over which services credentials can access.

## Credential Validation

MCP OAuth credentials support validation via `POST /vaults/{vault_id}/credentials/{credential_id}/mcp_oauth_validate`:

- **Checks**: Whether refresh token is present
- **Returns**: Validation status, timestamp, and optional HTTP response details
- **Purpose**: Verify credential configuration without attempting actual authentication [@vaults-handler]

Full OAuth flow validation requires making actual requests to the token endpoint, which is handled separately by the MCP tunnel system.

## Credential Constraints

Vault credentials have several limits:

- **Per vault**: Maximum 20 active credentials
- **Name requirements**: 1-255 characters for display names
- **Uniqueness**: Credential keys must be unique within a vault
- **URL limits**: MCP server URLs up to 2048 characters
- **Host validation**: Private IPs and localhost rejected unless insecure mode enabled [@vaults-handler]

These constraints ensure vaults remain manageable and secure while supporting realistic use cases.

## Webhook Events

Vaults emit webhook events for lifecycle changes:

- `vault.created`: New vault created
- `vault.archived`: Vault archived (credentials also archived)
- `vault.deleted`: Vault deleted (credentials also deleted)
- `vault_credential.created`: New credential added
- `vault_credential.archived`: Credential archived
- `vault_credential.deleted`: Credential deleted
- `vault_credential.refresh_failed`: OAuth refresh failure [@vaults-handler]

Events include vault context in delivery, enabling downstream systems to track credential lifecycle.

## API Endpoints

The vaults API requires `beta=true` query parameter and provides:

**Vault operations:**
- `POST /vaults`: Create a new vault
- `GET /vaults`: List vaults with pagination
- `GET /vaults/{vault_id}`: Retrieve a specific vault
- `POST /vaults/{vault_id}`: Update vault metadata
- `POST /vaults/{vault_id}/archive`: Archive a vault
- `DELETE /vaults/{vault_id}`: Delete a vault

**Credential operations:**
- `POST /vaults/{vault_id}/credentials`: Create a new credential
- `GET /vaults/{vault_id}/credentials`: List credentials in a vault
- `GET /vaults/{vault_id}/credentials/{credential_id}`: Retrieve a credential
- `POST /vaults/{vault_id}/credentials/{credential_id}`: Update credential
- `POST /vaults/{vault_id}/credentials/{credential_id}/archive`: Archive a credential
- `DELETE /vaults/{vault_id}/credentials/{credential_id}`: Delete a credential
- `POST /vaults/{vault_id}/credentials/{credential_id}/mcp_oauth_validate`: Validate MCP OAuth credential [@vaults-handler]

## Secret Handling

Secret values are handled specially for security:

- **Create**: Returned only in response immediately after creation
- **Update**: Never returned in responses (updates use patch semantics)
- **Storage**: Stored separately from public credential configuration
- **Archival**: Cleared when credential or vault is archived
- **Deletion**: Hard-deleted when credential is deleted [@vaults-handler]

Clients must store secret values securely when received and cannot retrieve them later through the API.

## Vault References

Vaults are referenced by deployments and sessions:

- **Deployments**: Include vault IDs in deployment configuration
- **Sessions**: Inherit vault references from parent deployment
- **Resolution**: At session creation, referenced vaults must exist and be active
- **Injection**: Credential secrets are injected into agent execution environments

When a deployment is archived, its vault references are preserved but the vaults themselves must still exist and be active for the deployment to be executed [@vaults-handler].
