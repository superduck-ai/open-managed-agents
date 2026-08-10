---
title: "Auth Tokens"
summary: "Authentication tokens identify principals across API requests, supporting API keys, environment keys, and platform sessions."
topics: [reference, authentication, security]
sources:
  - id: auth-package
    type: file
    path: internal/auth/auth.go
  - id: platformauth-service
    type: file
    path: internal/platformauth/service.go
  - id: codesessions-ingress
    type: file
    path: internal/codesessions/ingress.go
---

# Auth Tokens

Authentication tokens identify the principal making an API request and determine their authorization scope. The system supports multiple credential types including API keys, environment keys, and platform sessions[@auth-package].

# Credential Types

The system recognizes three credential types[@auth-package]:

- **`api_key`**: Standard API keys associated with workspaces
- **`environment_key`**: Keys scoped to specific environments with restricted permissions
- **`platform_session`**: Session-based authentication from platform login

# Principal Structure

The `Principal` struct encapsulates identity information extracted from a valid credential[@auth-package]:

```go
type Principal struct {
    CredentialType            string
    APIKeyID                  int64
    APIKeyExternalID          string
    OrganizationID            int64
    OrganizationUUID          string
    OrganizationExternalID    string
    WorkspaceID               int64
    WorkspaceUUID             string
    WorkspaceExternalID       string
    UserID                    int64
    UserExternalID            string
    PlatformSessionExternalID string
    EnvironmentKeyID          int64
    EnvironmentID             int64
    EnvironmentExternalID     string
}
```

# API Key Extraction

API keys are extracted from HTTP requests via the `ExtractAPIKey` function[@auth-package]:

1. Check `X-Api-Key` header first
2. Fall back to `Authorization` header with `Bearer` prefix
3. Return empty string if neither header contains a key

The key is then validated against the database to retrieve principal information.

# Platform Session Extraction

Platform session keys are extracted from cookies via the `ExtractPlatformSessionKey` function[@auth-package]:

```go
func ExtractPlatformSessionKey(r *http.Request) string {
    cookie, err := r.Cookie("sessionKey")
    if err != nil {
        return ""
    }
    return strings.TrimSpace(cookie.Value)
}
```

This allows web-based clients to authenticate using session cookies from platform login.

# Key Hashing

API keys are hashed using SHA-256 before storage for security[@auth-package]:

```go
func HashAPIKey(key string) string {
    return HashSecret(key)
}

func HashSecret(secret string) string {
    sum := sha256.Sum256([]byte(secret))
    return hex.EncodeToString(sum[:])
}
```

Only the hash is stored in the database; the raw key is never persisted.

# Context Propagation

The principal is attached to the request context using `WithPrincipal` and retrieved with `PrincipalFromContext`[@auth-package]:

```go
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
    return context.WithValue(ctx, contextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
    principal, ok := ctx.Value(contextKey{}).(Principal)
    return principal, ok
}
```

This pattern allows authentication to happen once at the request boundary, with authorization checks throughout the request handling using the context-attached principal.

# Platform Authentication Service

The `platformauth` service handles user context creation and resolution for platform-authenticated requests[@platformauth-service].

The `FindOrCreateUserContextByEmail` function[@platformauth-service]:

1. Normalizes the email address
2. Generates a default username from the email
3. Searches for existing user context by email
4. Creates a new organization, user, workspace, and API key if not found
5. Updates empty usernames with the generated default
6. Returns user external ID and organization UUID

The `ResolvePlatformSessionIdentity` function resolves platform session credentials to a full principal with all scoping information[@platformauth-service].

# Default Organization Creation

When creating a default organization for a new user, the system[@platformauth-service]:

1. Generates external IDs for organization, workspace, member, and API key
2. Creates the organization with a default name derived from the email
3. Creates a user with admin role
4. Creates a "default" workspace with a compartment ID
5. Creates a workspace member with workspace admin role
6. Creates a default API key with the format `sk-ant-api03-{32-char-random}`

The default organization name follows the pattern of the email's local part, with "Local Organization" as a fallback[@platformauth-service].

# Email Normalization

Email addresses are normalized for storage and lookup[@platformauth-service]:

1. Convert to lowercase
2. Trim whitespace
3. Fall back to "test@qq.com" if empty

Usernames are derived from the local part of the email, replacing dots and underscores with spaces and trimming to 80 characters[@platformauth-service].

# Partial API Key Hint

For logging and identification purposes, the system generates partial API key hints showing only the first 8 and last 4 characters[@platformauth-service]:

```go
func partialAPIKeyHint(key string) string {
    if len(key) <= 12 {
        return key
    }
    return key[:8] + "..." + key[len(key)-4:]
}
```

This allows identification without exposing full credentials in logs.

# Ingress Authorization

All API requests go through ingress authorization before handler execution[@codesessions-ingress]. The authorization process:

1. Extracts credentials from headers or cookies
2. Looks up the principal from the database
3. Attaches the principal to the request context
4. Returns 401 if credentials are missing or invalid
5. Returns 403 if the principal lacks required scope

Protected endpoints check the context for a valid principal before proceeding with the request.

# See Also

- [Permission Modes](/almanac/reference/permission-modes)
- [Worker API](/almanac/reference/worker-api)
