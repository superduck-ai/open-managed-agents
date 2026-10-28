import type { ApiError } from '../../shared/api/client';
import type { I18nContextValue } from '../../shared/i18n/context';

type Msg = I18nContextValue['msg'];

const DELIVERY_ERRORS: [RegExp, string, string][] = [
  [/^invalid destination URL$/, 'telemetry.delivery.invalidUrl', 'The destination URL is invalid'],
  [
    /^destination resolved to a non-public address$/,
    'telemetry.delivery.nonPublic',
    'The destination resolved to a non-public address',
  ],
  [
    /^destination request failed before a response$/,
    'telemetry.delivery.noResponse',
    'The destination could not be reached before a response',
  ],
  [
    /^destination redirected with status (\d+)$/,
    'telemetry.delivery.redirected',
    'The destination redirected ({status})',
  ],
  [/^destination returned status (\d+)$/, 'telemetry.delivery.status', 'The destination returned HTTP {status}'],
  [/^could not compress spans$/, 'telemetry.delivery.compress', 'Spans could not be compressed'],
];

export function deliveryErrorMessage(error: string, msg: Msg) {
  for (const [pattern, id, fallback] of DELIVERY_ERRORS) {
    const match = pattern.exec(error);
    if (match) return msg(id, fallback, { status: match[1] ?? '' });
  }
  return error;
}

function knownRequestError(error: Partial<ApiError>, msg: Msg) {
  if (error.status === 403) {
    return msg('telemetry.error.forbidden', 'Only organization administrators can change telemetry destinations.');
  }
  if (error.status === 409) {
    return msg('telemetry.error.conflict', 'The destination was changed elsewhere. Reload and try again.');
  }
  if (error.status === 429) {
    return msg('telemetry.error.testRateLimited', 'Wait at least 10 seconds between test spans.');
  }
  if (error.message?.startsWith('an organization can have at most')) {
    return msg('telemetry.limitReached', 'An organization can have at most 10 telemetry destinations.');
  }
  return undefined;
}

export function requestErrorMessage(error: unknown, summary: string, msg: Msg) {
  const apiError: Partial<ApiError> = error && typeof error === 'object' ? error : {};
  const known = knownRequestError(apiError, msg);
  if (known) return known;
  return typeof apiError.message === 'string' && apiError.message
    ? msg('telemetry.error.detail', '{summary}: {detail}', { summary, detail: apiError.message })
    : summary;
}
