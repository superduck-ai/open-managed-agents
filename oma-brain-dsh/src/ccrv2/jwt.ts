// Copied from superduck-code/src/bridge/jwtUtils.ts. Use native JSON.parse
// instead of the CLI's instrumentation wrapper; keep opaque payloads unknown.

/** Decode a JWT payload without verifying it, including the session-ingress prefix. */
export function decodeJwtPayload(token: string): unknown | null {
  const jwt = token.startsWith("sk-ant-si-") ? token.slice("sk-ant-si-".length) : token;
  const parts = jwt.split(".");
  if (parts.length !== 3 || !parts[1]) return null;
  try {
    return JSON.parse(Buffer.from(parts[1], "base64url").toString("utf8"));
  } catch {
    return null;
  }
}

/** Read exp in Unix seconds for failure classification, never authentication. */
export function decodeJwtExpiry(token: string): number | null {
  const payload = decodeJwtPayload(token);
  if (payload !== null && typeof payload === "object" &&
    "exp" in payload && typeof payload.exp === "number") return payload.exp;
  return null;
}
