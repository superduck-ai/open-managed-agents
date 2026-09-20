import { useId, useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Label } from '../../../shared/ui/label';
import { useI18n } from '../../../shared/i18n';
import { allWebhookEventTypes, orderedEvents, webhookEventGroups } from './events';
import { InlineError } from './feedback';

export function WebhookEventPicker({
  selected,
  onChange,
  disabled,
  initialEvents = [],
}: {
  initialEvents?: string[];
  selected: string[];
  onChange: (events: string[]) => void;
  disabled: boolean;
}) {
  const { msg } = useI18n();
  const pickerId = useId();
  const [copied, setCopied] = useState('');
  const [copyError, setCopyError] = useState(false);
  const extraEvents = initialEvents.filter((event) => !allWebhookEventTypes.includes(event));
  const groups = extraEvents.length
    ? [
        ...webhookEventGroups,
        { label: 'Other subscribed events', events: extraEvents.map((type) => ({ type, label: type })) },
      ]
    : webhookEventGroups;
  const availableTypes = groups.flatMap((group) => group.events.map((event) => event.type));

  const toggle = (types: string[]) => {
    const next = new Set(selected);
    const remove = types.every((type) => next.has(type));
    types.forEach((type) => (remove ? next.delete(type) : next.add(type)));
    onChange(orderedEvents(next));
  };
  const copyType = async (type: string) => {
    setCopyError(false);
    setCopied('');
    try {
      await navigator.clipboard.writeText(type);
      setCopied(type);
    } catch {
      setCopyError(true);
    }
  };

  return (
    <fieldset className="mt-6 min-w-0" disabled={disabled}>
      <legend className="mb-3 text-sm font-medium text-foreground">
        {msg('webhooks.eventsToSubscribe', 'Events to subscribe')}
      </legend>
      <div className="space-y-4 border-t border-border pt-3">
        <EventGroupToggle
          label={msg('webhooks.selectAll', 'Select all')}
          selectedCount={selected.length}
          total={availableTypes.length}
          disabled={disabled}
          onChange={() => toggle(availableTypes)}
        />
        {groups.map((group) => (
          <div key={group.label}>
            <EventGroupToggle
              label={`${group.label} events`}
              displayLabel={group.label}
              selectedCount={group.events.filter((event) => selected.includes(event.type)).length}
              total={group.events.length}
              disabled={disabled}
              onChange={() => toggle(group.events.map((event) => event.type))}
            />
            <div className="ml-6 mt-2 space-y-2">
              {group.events.map((event) => (
                <div key={event.type} className="flex min-w-0 items-center gap-2">
                  <div className="flex min-w-0 flex-1 items-center gap-2 text-sm text-foreground">
                    <Checkbox
                      id={`${pickerId}-${event.type}-checkbox`}
                      checked={selected.includes(event.type)}
                      aria-labelledby={`${pickerId}-${event.type}`}
                      disabled={disabled}
                      onCheckedChange={() => toggle([event.type])}
                    />
                    <Label htmlFor={`${pickerId}-${event.type}-checkbox`} className="block min-w-0 leading-5">
                      <span className="block">{event.label}</span>
                      <span
                        id={`${pickerId}-${event.type}`}
                        className="block break-all font-mono text-xs text-muted-foreground"
                      >
                        {event.type}
                      </span>
                    </Label>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={msg('webhooks.copyEventType', 'Copy {eventType}', { eventType: event.type })}
                    onClick={() => void copyType(event.type)}
                    disabled={disabled}
                  >
                    {copied === event.type ? <Check aria-hidden /> : <Copy aria-hidden />}
                  </Button>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
      <span className="sr-only" role="status">
        {copied ? msg('common.copied', 'Copied') : ''}
      </span>
      {copyError ? (
        <InlineError>
          {msg('webhooks.copyFailed', 'Could not copy. Please select and copy the text manually.')}
        </InlineError>
      ) : null}
    </fieldset>
  );
}

function EventGroupToggle({
  label,
  displayLabel,
  selectedCount,
  total,
  disabled,
  onChange,
}: {
  label: string;
  displayLabel?: string;
  selectedCount: number;
  total: number;
  disabled: boolean;
  onChange: () => void;
}) {
  const labelId = useId();
  return (
    <div className="flex min-h-7 items-center justify-between gap-3 text-sm">
      <div className="flex min-w-0 items-center gap-2 font-medium">
        <Checkbox
          id={`${labelId}-checkbox`}
          aria-labelledby={labelId}
          checked={selectedCount === total}
          indeterminate={selectedCount > 0 && selectedCount < total}
          disabled={disabled}
          onCheckedChange={onChange}
        />
        <Label htmlFor={`${labelId}-checkbox`}>
          <span aria-hidden>{displayLabel ?? label}</span>
          <span className="sr-only" id={labelId}>
            {label}
          </span>
        </Label>
      </div>
      <span className="shrink-0 text-xs text-muted-foreground" aria-live="polite">
        {selectedCount} of {total}
      </span>
    </div>
  );
}
