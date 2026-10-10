import { CalendarDays, Hand, Search } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { ResourceListState } from '../../../shared/ui/resource-list-state';
import { type DeploymentApiResponse } from '../types';
import { objectRecord } from '../utils';
import { cronToSchedule } from './deployment-schedule';

export function DeploymentTriggerCell({ deployment }: { deployment: DeploymentApiResponse }) {
  const { msg, locale } = useI18n();
  const schedule = objectRecord(deployment.schedule);
  const expression = typeof schedule.expression === 'string' ? schedule.expression.trim() : '';
  const timezone = typeof schedule.timezone === 'string' ? schedule.timezone : '';
  const scheduled = expression !== '';
  const controls = cronToSchedule(expression);
  const [hour, minute] = controls.time.split(':').map(Number);
  const clock = new Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit', timeZone: 'UTC' }).format(
    new Date(Date.UTC(2026, 0, 1, hour, minute)),
  );
  const day = new Intl.DateTimeFormat(locale, { weekday: 'short', timeZone: 'UTC' }).format(
    new Date(Date.UTC(2026, 8, 6 + Number(controls.day))),
  );
  const labels = {
    minute: msg('managedAgents.deployments.schedule.minute', 'Every minute'),
    hour: msg('managedAgents.deployments.schedule.hourlyAt', 'Hourly at :{minute}', {
      minute: controls.time.split(':')[1],
    }),
    daily: msg('managedAgents.deployments.schedule.dailyAt', 'Daily at {time}', { time: clock }),
    weekdays: msg('managedAgents.deployments.schedule.weekdaysAt', 'Weekdays at {time}', { time: clock }),
    weekly: msg('managedAgents.deployments.schedule.weeklyAt', '{day} at {time}', { day, time: clock }),
    custom: expression,
  };
  const triggerLabel = scheduled
    ? labels[controls.frequency] || msg('managedAgents.deployments.schedule.invalid', 'Invalid schedule')
    : msg('managedAgents.deployments.trigger.manual', 'Manual');
  const Icon = scheduled ? CalendarDays : Hand;
  return (
    <div className="flex min-w-0 items-start gap-2" title={scheduled ? `${expression} · ${timezone}` : undefined}>
      <Icon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <div className="min-w-0">
        <div className="truncate">{triggerLabel}</div>
        {scheduled && timezone ? <div className="mt-1 truncate text-xs text-muted-foreground">{timezone}</div> : null}
      </div>
    </div>
  );
}

export function DeploymentEmptyState({ filtered }: { filtered: boolean }) {
  const { msg } = useI18n();
  return (
    <ResourceListState
      icon={filtered ? Search : CalendarDays}
      title={
        filtered
          ? msg('managedAgents.deployments.noMatches', 'No matching deployments')
          : msg('managedAgents.deployments.emptyTitle', 'No deployments yet')
      }
      body={
        filtered
          ? msg('managedAgents.deployments.noMatchesHelp', 'Try another name or adjust your filters.')
          : msg(
              'managedAgents.deployments.emptyBody',
              'Create a deployment to bind an agent to credentials, an environment, and a schedule.',
            )
      }
    />
  );
}
