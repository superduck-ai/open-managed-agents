import { CalendarDays, Hand, Plus, Search } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../../shared/ui/empty';
import { type DeploymentApiResponse } from '../types';
import { objectRecord } from '../utils';
import { cronToSchedule } from './deployment-schedule';

export function DeploymentTriggerCell({ deployment }: { deployment: DeploymentApiResponse }) {
  const { msg, locale } = useI18n();
  const schedule = objectRecord(deployment.schedule);
  const expression = typeof schedule.expression === 'string' ? schedule.expression : '';
  const timezone = typeof schedule.timezone === 'string' ? schedule.timezone : '';
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
  const Icon = deployment.schedule ? CalendarDays : Hand;
  return (
    <div className="flex min-w-0 items-start gap-2" title={expression ? `${expression} · ${timezone}` : undefined}>
      <Icon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <div className="min-w-0">
        <div className="truncate">
          {deployment.schedule ? labels[controls.frequency] : msg('managedAgents.deployments.trigger.manual', 'Manual')}
        </div>
        {timezone && <div className="mt-1 truncate text-xs text-muted-foreground">{timezone}</div>}
      </div>
    </div>
  );
}

export function DeploymentEmptyState({ filtered, onCreate }: { filtered: boolean; onCreate: () => void }) {
  const { msg } = useI18n();
  return (
    <Empty className="min-h-72 rounded-none py-12">
      <EmptyHeader>
        <EmptyMedia variant="icon" className="size-10">
          {filtered ? <Search /> : <CalendarDays />}
        </EmptyMedia>
        <EmptyTitle>
          {filtered
            ? msg('managedAgents.deployments.noMatches', 'No matching deployments')
            : msg('managedAgents.deployments.emptyTitle', 'No deployments yet')}
        </EmptyTitle>
        <EmptyDescription>
          {filtered
            ? msg('managedAgents.deployments.noMatchesHelp', 'Try another name or adjust your filters.')
            : msg('managedAgents.deployments.emptyBody', 'Deployments will appear after an agent is deployed.')}
        </EmptyDescription>
      </EmptyHeader>
      {!filtered && (
        <Button variant="outline" onClick={onCreate}>
          <Plus aria-hidden />
          {msg('managedAgents.deployments.createLabel', 'Create deployment')}
        </Button>
      )}
    </Empty>
  );
}
