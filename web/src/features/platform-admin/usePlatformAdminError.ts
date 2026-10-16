import { useI18n } from '../../shared/i18n';
import type { ApiError } from '../../shared/api/client';

export function usePlatformAdminError() {
  const { msg } = useI18n();
  const messages: Record<string, string> = {
    last_platform_admin: msg('platformAdmin.errors.lastPlatformAdmin', 'At least one super administrator must remain.'),
    last_organization_admin: msg(
      'platformAdmin.errors.lastOrganizationAdmin',
      'At least one organization administrator must remain.',
    ),
    version_conflict: msg(
      'platformAdmin.errors.versionConflict',
      'Organization details changed. Refresh and try again.',
    ),
    idempotency_conflict: msg(
      'platformAdmin.errors.idempotencyConflict',
      'This request key was already used for different organization details.',
    ),
    state_conflict: msg('platformAdmin.errors.stateConflict', 'This action is not available in the current state.'),
    rate_limited: msg('platformAdmin.errors.rateLimited', 'Too many requests. Please try again later.'),
    invalid_request: msg('platformAdmin.errors.invalidRequest', 'Check the form values and try again.'),
    not_found: msg('platformAdmin.errors.notFound', 'The requested resource was not found.'),
    platform_access_denied: msg(
      'platformAdmin.errors.denied',
      'Only super administrators can access platform administration.',
    ),
    verified_login_required: msg(
      'platformAdmin.errors.verifiedLogin',
      'Sign in again with an email verification code.',
    ),
    invalid_csrf: msg('platformAdmin.errors.csrf', 'Request verification failed. Refresh the page.'),
    unauthenticated: msg('platformAdmin.errors.unauthenticated', 'Please sign in again.'),
    unavailable: msg('platformAdmin.errors.unavailable', 'Platform administration is temporarily unavailable.'),
  };
  return (error: unknown) =>
    messages[(error as ApiError)?.code ?? ''] ??
    msg('platformAdmin.errors.requestFailed', 'The request failed. Please try again.');
}
