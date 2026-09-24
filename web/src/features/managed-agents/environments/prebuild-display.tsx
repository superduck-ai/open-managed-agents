import { Check, Circle, CircleHelp, LoaderCircle, TriangleAlert } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { prebuildIsActive, type PrebuildState, type EnvironmentPrebuild } from './prebuild';

const stateMessages = {
  idle: ['environmentPrebuild.idle', 'Not preinstalled'],
  queued: ['environmentPrebuild.queued', 'Queued'],
  submitting: ['environmentPrebuild.preparing', 'Preparing'],
  running: ['environmentPrebuild.preparing', 'Preparing'],
  canceling: ['environmentPrebuild.canceling', 'Canceling'],
  cancelled: ['environmentPrebuild.cancelled', 'Canceled'],
  failed: ['environmentPrebuild.failed', 'Preparation failed'],
  unknown: ['environmentPrebuild.unknown', 'Status unconfirmed'],
  ready: ['environmentPrebuild.ready', 'Preinstalled'],
} as const;

const stateIcons: Partial<Record<PrebuildState, typeof Check>> = {
  ready: Check,
  failed: TriangleAlert,
  unknown: CircleHelp,
};

export function PrebuildIcon({ state }: { state: PrebuildState }) {
  const Icon = prebuildIsActive(state) ? LoaderCircle : stateIcons[state] || Circle;
  return (
    <Icon
      aria-hidden
      className={`size-3.5 shrink-0 ${prebuildIsActive(state) ? 'animate-spin motion-reduce:animate-none' : ''}`}
      strokeWidth={1.5}
    />
  );
}

export function PrebuildStateText({ state }: { state: PrebuildState }) {
  const { msg } = useI18n();
  const [id, fallback] = stateMessages[state] || stateMessages.unknown;
  return <>{msg(id, fallback)}</>;
}

export function PrebuildLabel({ state }: { state: PrebuildState }) {
  return (
    <span className={`inline-flex items-center gap-2 ${state === 'failed' ? 'text-(--warning)' : ''}`}>
      <PrebuildIcon state={state} />
      <span>
        <PrebuildStateText state={state} />
      </span>
    </span>
  );
}

export function PrebuildTime({ prebuild }: { prebuild: EnvironmentPrebuild }) {
  const { locale } = useI18n();
  if (!prebuild.createdAt) return null;
  return (
    <time dateTime={prebuild.createdAt} title={new Date(prebuild.createdAt).toLocaleString(locale)}>
      {new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(
        new Date(prebuild.createdAt),
      )}
    </time>
  );
}

export function PrebuildDuration({ prebuild }: { prebuild: EnvironmentPrebuild }) {
  const { locale } = useI18n();
  if (!prebuild.createdAt || (!prebuild.finishedAt && !prebuildIsActive(prebuild.state))) return <>—</>;
  const seconds = Math.max(
    0,
    Math.floor(
      ((prebuild.finishedAt ? Date.parse(prebuild.finishedAt) : Date.now()) - Date.parse(prebuild.createdAt)) / 1000,
    ),
  );
  const format = (value: number, unit: string) =>
    new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'narrow' }).format(value);
  return (
    <>
      {seconds < 60
        ? format(seconds, 'second')
        : `${format(Math.floor(seconds / 60), 'minute')} ${format(seconds % 60, 'second')}`}
    </>
  );
}
