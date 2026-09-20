import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '../../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import { useI18n } from '../../../shared/i18n';
import type { CreateWebhookEndpointInput, UpdateWebhookEndpointInput, WebhookEndpoint } from '../webhooksApi';
import { orderedEvents } from './events';
import { InlineError, readableError } from './feedback';
import { WebhookEventPicker } from './WebhookEventPicker';
import { WebhookFields, isValidWebhookUrl } from './WebhookFields';

export function WebhookDetailEditForm({
  webhook,
  isSubmitting,
  error,
  onCancel,
  onSave,
}: {
  webhook: WebhookEndpoint;
  isSubmitting: boolean;
  error?: string | null;
  onCancel: () => void;
  onSave: (input: UpdateWebhookEndpointInput) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [url, setUrl] = useState(webhook.url);
  const [name, setName] = useState(webhook.name ?? '');
  const [description, setDescription] = useState(webhook.description ?? '');
  const [selectedEvents, setSelectedEvents] = useState<string[]>(() => orderedEvents(new Set(webhook.enabled_events)));
  const canSubmit = isValidWebhookUrl(url) && selectedEvents.length > 0 && !isSubmitting;

  useEffect(() => {
    setUrl(webhook.url);
    setName(webhook.name ?? '');
    setDescription(webhook.description ?? '');
    setSelectedEvents(orderedEvents(new Set(webhook.enabled_events)));
  }, [webhook]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    await onSave({
      url: url.trim(),
      name: name.trim(),
      description: description.trim(),
      enabled_events: selectedEvents,
    });
  };

  return (
    <form className="border-t border-border pt-8" onSubmit={(event) => void handleSubmit(event).catch(() => undefined)}>
      <WebhookFields
        prefix="webhook-detail"
        url={url}
        name={name}
        description={description}
        onUrl={setUrl}
        onName={setName}
        onDescription={setDescription}
        disabled={isSubmitting}
      />

      <WebhookEventPicker
        selected={selectedEvents}
        initialEvents={webhook.enabled_events}
        onChange={setSelectedEvents}
        disabled={isSubmitting}
      />

      {error ? <InlineError>{error}</InlineError> : null}

      <div className="mt-6 flex justify-end gap-2">
        <Button type="button" variant="outline" size="lg" onClick={onCancel} disabled={isSubmitting}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button type="submit" disabled={!canSubmit} size="lg" className="min-w-[82px]">
          {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
          {msg('common.save', 'Save')}
        </Button>
      </div>
    </form>
  );
}

export function CreateWebhookDialog({
  open,
  isSubmitting,
  onClose,
  onCreate,
}: {
  open: boolean;
  isSubmitting: boolean;
  onClose: () => void;
  onCreate: (input: CreateWebhookEndpointInput) => Promise<void>;
}) {
  const { msg } = useI18n();
  const urlRef = useRef<HTMLInputElement>(null);
  const [url, setUrl] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selectedEvents, setSelectedEvents] = useState<string[]>([]);
  const [error, setError] = useState('');
  const canSubmit = isValidWebhookUrl(url) && selectedEvents.length > 0 && !isSubmitting;

  useEffect(() => {
    if (!open) {
      setUrl('');
      setName('');
      setDescription('');
      setSelectedEvents([]);
      setError('');
    }
  }, [open]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setError('');
    try {
      await onCreate({
        url: url.trim(),
        name: name.trim(),
        description: description.trim(),
        enabled_events: selectedEvents,
      });
    } catch (createError) {
      setError(readableError(createError) ?? msg('webhooks.createFailed', 'Failed to create webhook endpoint.'));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !isSubmitting) {
          onClose();
        }
      }}
    >
      {/* Header/footer keep content height via auto grid rows; the minmax(0,1fr) form row absorbs overflow
          inside the max-h cap instead of hardcoded pixel budgets, and min-h-0 lets the scroll area shrink. */}
      <DialogContent
        className="grid-rows-[auto_minmax(0,1fr)] max-h-[min(720px,calc(100vh-48px))] gap-0 overflow-hidden p-0 sm:max-w-[540px]"
        initialFocus={urlRef}
      >
        <DialogHeader className="px-4 py-4">
          <DialogTitle>{msg('webhooks.createTitle', 'Create webhook endpoint')}</DialogTitle>
        </DialogHeader>
        <form className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto]" onSubmit={handleSubmit}>
          <div className="subtle-scrollbar-auto min-h-0 space-y-4 overflow-y-auto pl-4 pr-2 py-4">
            <WebhookFields
              prefix="webhook"
              url={url}
              name={name}
              description={description}
              onUrl={setUrl}
              onName={setName}
              onDescription={setDescription}
              disabled={isSubmitting}
              urlRef={urlRef}
            />

            <WebhookEventPicker selected={selectedEvents} onChange={setSelectedEvents} disabled={isSubmitting} />
          </div>

          <div className="px-4">
            {error ? <InlineError>{error}</InlineError> : null}
            <DialogFooter className="py-4">
              <Button type="submit" disabled={!canSubmit} size="lg" className="min-w-[82px]">
                {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                {msg('common.create', 'Create')}
              </Button>
            </DialogFooter>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
