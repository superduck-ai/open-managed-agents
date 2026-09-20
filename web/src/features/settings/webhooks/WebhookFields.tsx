import type { RefObject } from 'react';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { Textarea } from '../../../shared/ui/textarea';
import { useI18n } from '../../../shared/i18n';

export function isValidWebhookUrl(value: string) {
  try {
    const input = value.trim();
    if (!/^https:\/\//i.test(input) || /[\s\\]/.test(input)) return false;
    const url = new URL(input);
    return (
      value.trim().length <= 2048 &&
      url.protocol === 'https:' &&
      Boolean(url.hostname) &&
      (!url.port || url.port === '443') &&
      !url.username &&
      !url.password &&
      !url.hash
    );
  } catch {
    return false;
  }
}

export function WebhookFields({
  prefix,
  url,
  name,
  description,
  onUrl,
  onName,
  onDescription,
  disabled,
  urlRef,
}: {
  prefix: string;
  url: string;
  name: string;
  description: string;
  onUrl: (value: string) => void;
  onName: (value: string) => void;
  onDescription: (value: string) => void;
  disabled: boolean;
  urlRef?: RefObject<HTMLInputElement | null>;
}) {
  const { msg } = useI18n();
  const invalidUrl = url.length > 0 && !isValidWebhookUrl(url);
  return (
    <fieldset disabled={disabled} className="min-w-0 space-y-4">
      <Label htmlFor={`${prefix}-url`}>{msg('webhooks.endpointUrl', 'Endpoint URL')}</Label>
      <Input
        ref={urlRef}
        id={`${prefix}-url`}
        value={url}
        maxLength={2048}
        placeholder="https://example.com/webhooks"
        onChange={(event) => onUrl(event.target.value)}
        aria-invalid={invalidUrl}
        aria-describedby={invalidUrl ? `${prefix}-url-error` : undefined}
      />
      {invalidUrl ? (
        <p id={`${prefix}-url-error`} role="alert" className="text-sm text-destructive">
          {msg('webhooks.invalidUrl', 'Must be a valid HTTPS URL on port 443, without credentials or a fragment.')}
        </p>
      ) : null}
      <Label htmlFor={`${prefix}-name`}>{msg('webhooks.nameOptional', 'Name (optional)')}</Label>
      <Input
        id={`${prefix}-name`}
        value={name}
        maxLength={255}
        placeholder={msg('webhooks.namePlaceholder', 'My webhook endpoint')}
        onChange={(event) => onName(event.target.value)}
      />
      <Label htmlFor={`${prefix}-description`}>{msg('webhooks.descriptionOptional', 'Description (optional)')}</Label>
      <Textarea
        id={`${prefix}-description`}
        value={description}
        maxLength={2048}
        placeholder={msg('webhooks.descriptionPlaceholder', 'Receives session lifecycle events')}
        className="min-h-[78px] resize-y"
        onChange={(event) => onDescription(event.target.value)}
      />
    </fieldset>
  );
}
