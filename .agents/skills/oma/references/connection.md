# OMA connection configuration

## Endpoint and credentials

Use the user's explicit API endpoint, otherwise `OMA_BASE_URL`. Ask if neither exists; do not assume a deployment domain. Use the API origin without `/v1`; remove a trailing slash. Reject embedded credentials, query strings, fragments, and non-root paths until a deployment explicitly supports a path prefix.

Use HTTPS for remote deployments. HTTP is appropriate for explicitly selected loopback development endpoints. Do not disable certificate validation or forward credentials to a different origin.

Use the key supplied for this endpoint, otherwise an `OMA_API_KEY` explicitly associated with this origin and workspace. Reconfirm credentials when switching instances. Do not silently reuse an official Anthropic login, cloud profile, or another instance's key. Never write real keys into skill files, agent definitions, lockfiles, logs, or Git.

## Configure ant and SDKs

In an isolated Bash session, replace the placeholder and enter the target instance's key:

```bash
export OMA_BASE_URL='https://your-oma-host'
printf 'OMA API key: '
IFS= read -r -s OMA_API_KEY
printf '\n'
export OMA_API_KEY
export ANTHROPIC_BASE_URL="${OMA_BASE_URL%/}"
export ANTHROPIC_API_KEY="$OMA_API_KEY"
ant models list --format jsonl
```

OMA variables identify the connection; the upstream ant CLI uses the mapped ANTHROPIC variables. Check for conflicting cloud routing or profiles without displaying secrets or changing global credentials. Missing configuration in a noninteractive invocation is an error, not an invitation to wait for input.

Follow the upstream language-specific SDK examples. Pass the configured base URL through that SDK's documented client option when necessary. Continue using the official Anthropic SDK and compatible API shapes.

A successful model listing establishes basic connectivity and authentication, not model suitability or all write permissions. For 401, check the selected key; for 403, check workspace and permissions; for TLS/network failures, check the endpoint and connection. Do not probe other domains with the same key.

## Switching and cleanup

Keep separate resource directories and claude-lock.json files for each instance/workspace. Resource IDs from an old deployment are not portable to a new one.

Users create and rotate platform keys through the target OMA management interface. Validate a replacement before the user revokes the old key. Integration credentials for MCP/Vault remain separate from the platform key.

Exit the isolated shell or clear temporary configuration:

```bash
unset OMA_API_KEY ANTHROPIC_API_KEY OMA_BASE_URL ANTHROPIC_BASE_URL
```
