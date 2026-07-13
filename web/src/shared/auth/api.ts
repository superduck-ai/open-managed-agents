import { consoleApi } from '../api/client';

export type AuthAccount = {
  uuid: string;
  tagged_id?: string;
  email_address: string;
  full_name?: string;
  display_name?: string;
  memberships?: AuthMembership[];
};

export type AuthMembership = {
  role?: string;
  seat_tier?: string;
  organization?: {
    uuid?: string;
    name?: string;
    settings?: {
      default_workspace_settings?: {
        enable_api_keys?: boolean;
      };
    };
  };
};

export type BootstrapResponse = {
  account: AuthAccount | null;
  csrf_token?: string;
};

export type SendMagicLinkResponse = {
  sent: boolean;
  fallback_code_configuration: unknown | null;
  sso_url: string | null;
  magic_link_intent_available: boolean | null;
};

export type VerifyMagicLinkResponse = {
  success: boolean;
  created?: boolean;
  account?: AuthAccount;
};

export function fetchBootstrap() {
  return consoleApi<BootstrapResponse>('/api/bootstrap');
}

export function sendMagicLink(emailAddress: string) {
  return consoleApi<SendMagicLinkResponse>('/api/auth/send_magic_link', {
    method: 'POST',
    body: JSON.stringify({ email_address: emailAddress }),
  });
}

export function verifyMagicLink(emailAddress: string, code: string) {
  return consoleApi<VerifyMagicLinkResponse>('/api/auth/verify_magic_link', {
    method: 'POST',
    body: JSON.stringify({
      credentials: {
        method: 'code',
        code,
        email_address: emailAddress,
      },
    }),
  });
}

export function logout() {
  return consoleApi<{ ok: boolean }>('/api/auth/logout', {
    method: 'POST',
  });
}
