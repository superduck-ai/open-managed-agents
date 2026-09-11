export type MCPServerInputError = 'required' | 'too_long' | 'invalid' | 'ambiguous';

export type MCPServerInputErrors = {
  name?: MCPServerInputError;
  url?: Exclude<MCPServerInputError, 'ambiguous'>;
};

export function validateMCPServerInput(name: string, url: string): MCPServerInputErrors {
  return {
    ...validateMCPServerName(name.trim()),
    ...validateMCPServerURL(url.trim()),
  };
}

function validateMCPServerName(name: string): Pick<MCPServerInputErrors, 'name'> {
  if (!name) {
    return { name: 'required' };
  }
  if (name.length > 255) {
    return { name: 'too_long' };
  }
  if (!/^[A-Za-z0-9_.-]+$/.test(name)) {
    return { name: 'invalid' };
  }
  return isUnambiguousMCPServerName(name) ? {} : { name: 'ambiguous' };
}

function validateMCPServerURL(url: string): Pick<MCPServerInputErrors, 'url'> {
  if (!url) {
    return { url: 'required' };
  }
  if (new TextEncoder().encode(url).length > 2048) {
    return { url: 'too_long' };
  }
  return isHTTPMCPServerURL(url) ? {} : { url: 'invalid' };
}

export function isUnambiguousMCPServerName(value: string) {
  return !value.includes('__');
}

export function isHTTPMCPServerURL(value: string) {
  try {
    const parsed = new URL(value);
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
      Boolean(parsed.hostname) &&
      !parsed.username &&
      !parsed.password &&
      !parsed.hash
    );
  } catch {
    return false;
  }
}
