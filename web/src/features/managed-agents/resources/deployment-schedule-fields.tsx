import { CalendarDays, Clock3, Hand, Info } from 'lucide-react';
import { useEffect, useId, useMemo, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '../../../shared/ui/combobox';
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { DeploymentTimePicker } from './deployment-time-picker';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { DeploymentSelectField } from '../components/common';
import { type ManagedEntityFormValues } from '../types';
import {
  cronToSchedule,
  previewSchedule,
  scheduleFrequencies,
  scheduleToCron,
  type ScheduleControls,
} from './deployment-schedule';

function TimezoneField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { msg, locale } = useI18n();
  const id = useId();
  const zones = useMemo(
    () => [...new Set([value, 'UTC', ...Intl.supportedValuesOf('timeZone')])].filter(Boolean),
    [value],
  );
  const offset = useMemo(() => {
    try {
      return new Intl.DateTimeFormat(locale, { timeZone: value, timeZoneName: 'longOffset' })
        .formatToParts(new Date())
        .find((part) => part.type === 'timeZoneName')?.value;
    } catch {
      return '';
    }
  }, [locale, value]);
  return (
    <Field className="min-w-0 gap-1.5">
      <FieldLabel htmlFor={id}>{msg('managedAgents.deployments.timezone', 'Timezone')}</FieldLabel>
      <Combobox items={zones} value={value} onValueChange={(zone) => zone && onChange(zone)}>
        <ComboboxInput id={id} className="h-9 [&_[data-slot=input-group-addon]]:mr-0" />
        <ComboboxContent>
          <ComboboxEmpty>
            {msg('managedAgents.deployments.schedule.noTimezones', 'No matching timezones.')}
          </ComboboxEmpty>
          <ComboboxList>
            {(zone: string) => (
              <ComboboxItem key={zone} value={zone}>
                {zone}
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      {offset && <FieldDescription className="text-xs">{offset}</FieldDescription>}
    </Field>
  );
}

export function DeploymentScheduleFields({
  values,
  onChange,
}: {
  values: ManagedEntityFormValues;
  onChange: (patch: Partial<ManagedEntityFormValues>) => void;
}) {
  const { msg, locale } = useI18n();
  const id = useId();
  const [controls, setControls] = useState(() => cronToSchedule(values.cronExpression));
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 60_000);
    return () => window.clearInterval(timer);
  }, []);
  const preview = useMemo(
    () =>
      values.triggerType === 'schedule'
        ? previewSchedule(values.cronExpression, values.timezone, now)
        : { runs: [], error: null },
    [values.cronExpression, values.timezone, values.triggerType, now],
  );
  const frequencyLabels = {
    minute: msg('managedAgents.deployments.schedule.minute', 'Every minute'),
    hour: msg('managedAgents.deployments.schedule.hour', 'Every hour'),
    daily: msg('managedAgents.deployments.schedule.daily', 'Daily'),
    weekdays: msg('managedAgents.deployments.schedule.weekdays', 'Weekdays'),
    weekly: msg('managedAgents.deployments.schedule.weekly', 'Weekly'),
    custom: msg('managedAgents.deployments.schedule.custom', 'Custom cron'),
  };
  const changeControls = (patch: Partial<ScheduleControls>) => {
    const next = { ...controls, ...patch };
    setControls(next);
    if (next.frequency !== 'custom') onChange({ cronExpression: scheduleToCron(next) });
  };
  const dateFormatter = new Intl.DateTimeFormat(locale, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZone: preview.error === 'timezone' || !values.timezone ? 'UTC' : values.timezone,
  });
  const error =
    preview.error === 'timezone'
      ? msg('managedAgents.deployments.schedule.invalidTimezone', 'Choose a valid timezone.')
      : msg(
          'managedAgents.deployments.schedule.invalidCron',
          'Enter a valid five-field cron expression with a future run.',
        );
  return (
    <Tabs
      value={values.triggerType || 'manual'}
      onValueChange={(value) => onChange({ triggerType: value === 'schedule' ? 'schedule' : 'manual' })}
      className="gap-5"
    >
      <TabsList className="w-full" aria-label={msg('managedAgents.common.trigger', 'Trigger')}>
        <TabsTrigger value="manual">
          <Hand aria-hidden />
          {msg('managedAgents.deployments.trigger.manual', 'Manual')}
        </TabsTrigger>
        <TabsTrigger value="schedule">
          <CalendarDays aria-hidden />
          {msg('managedAgents.deployments.trigger.scheduled', 'Scheduled')}
        </TabsTrigger>
      </TabsList>
      <TabsContent value="manual">
        <div className="flex gap-3 rounded-lg border border-border bg-muted/40 p-4 text-sm leading-6 text-muted-foreground">
          <Info className="mt-1 size-4 shrink-0" aria-hidden />
          <p>
            {msg(
              'managedAgents.deployments.schedule.manualHelp',
              'Start a run whenever you need it using Run now on the deployment.',
            )}
          </p>
        </div>
      </TabsContent>
      <TabsContent value="schedule" className="space-y-5">
        <div className="grid items-start gap-4 sm:grid-cols-2">
          <DeploymentSelectField
            label={msg('managedAgents.deployments.schedule.frequency', 'Frequency')}
            value={controls.frequency}
            placeholder=""
            options={scheduleFrequencies.map((frequency) => ({ id: frequency, label: frequencyLabels[frequency] }))}
            onChange={(frequency) => {
              const selected = scheduleFrequencies.find((item) => item === frequency);
              if (selected) changeControls({ frequency: selected });
            }}
          />
          <TimezoneField value={values.timezone} onChange={(timezone) => onChange({ timezone })} />
          {controls.frequency === 'weekly' && (
            <DeploymentSelectField
              label={msg('managedAgents.deployments.schedule.on', 'On')}
              value={controls.day}
              placeholder=""
              options={Array.from({ length: 7 }, (_, day) => ({
                id: String(day),
                label: new Intl.DateTimeFormat(locale, { weekday: 'long', timeZone: 'UTC' }).format(
                  new Date(Date.UTC(2026, 8, 6 + day)),
                ),
              }))}
              onChange={(day) => day && changeControls({ day })}
            />
          )}
          {controls.frequency !== 'custom' && controls.frequency !== 'minute' && (
            <Field className="gap-1.5">
              <FieldLabel htmlFor={`${id}-time`}>
                {controls.frequency === 'hour'
                  ? msg('managedAgents.deployments.schedule.minuteOfHour', 'At minute')
                  : msg('managedAgents.deployments.schedule.at', 'At')}
              </FieldLabel>
              {controls.frequency === 'hour' ? (
                <Input
                  id={`${id}-time`}
                  type="number"
                  min={0}
                  max={59}
                  required
                  value={controls.time.split(':')[1]}
                  onChange={(event) =>
                    changeControls({
                      time: event.target.value === '' ? '' : `09:${event.target.value.padStart(2, '0')}`,
                    })
                  }
                />
              ) : (
                <DeploymentTimePicker
                  id={`${id}-time`}
                  value={controls.time}
                  onChange={(time) => changeControls({ time })}
                />
              )}
            </Field>
          )}
        </div>
        {controls.frequency === 'custom' ? (
          <Field className="gap-2" data-invalid={Boolean(preview.error)}>
            <FieldLabel htmlFor={`${id}-cron`}>
              {msg('managedAgents.deployments.cronExpression', 'Cron expression')}
            </FieldLabel>
            <Input
              id={`${id}-cron`}
              className="font-mono"
              value={values.cronExpression}
              aria-invalid={Boolean(preview.error)}
              aria-describedby={`${id}-cron-help`}
              onChange={(event) => onChange({ cronExpression: event.target.value })}
              placeholder="0 9 * * 1-5"
            />
            <FieldDescription id={`${id}-cron-help`}>
              {msg(
                'managedAgents.deployments.schedule.cronHelp',
                'Minute · hour · day of month · month · day of week (0–7, Sunday = 0 or 7).',
              )}
            </FieldDescription>
          </Field>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-muted/60 px-3 py-2">
            <code className="text-xs text-muted-foreground">{values.cronExpression || '—'}</code>
            <Button
              type="button"
              size="sm"
              variant="link"
              className="h-auto p-0"
              onClick={() => changeControls({ frequency: 'custom' })}
            >
              {msg('managedAgents.deployments.schedule.editCron', 'Edit cron')}
            </Button>
          </div>
        )}
        {preview.error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : (
          <div className="rounded-lg border border-border bg-muted/30 p-4" aria-live="polite">
            <p className="mb-3 text-sm font-medium">
              {msg('managedAgents.deployments.schedule.nextRuns', 'Next 5 runs')}
            </p>
            <ol className="space-y-2.5 text-sm text-muted-foreground">
              {preview.runs.map((run) => (
                <li key={run.toISOString()} className="flex items-center gap-2.5">
                  <Clock3 className="size-3.5 shrink-0" aria-hidden />
                  <time dateTime={run.toISOString()}>{dateFormatter.format(run)}</time>
                </li>
              ))}
            </ol>
          </div>
        )}
      </TabsContent>
    </Tabs>
  );
}
