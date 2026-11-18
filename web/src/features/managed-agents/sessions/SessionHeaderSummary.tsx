import clsx from 'clsx';
import { ResourceIdButton } from '../components/ResourceIdButton';
import type { SessionApiResponse } from '../types';
import type { buildSessionDetailSummary } from './sessionDetailModel';
import { SessionStatusPill, SessionSummaryChip } from './sessionTimeline';

export function SessionHeaderSummary({
  session,
  summary,
  archivedLabel,
}: {
  session: SessionApiResponse;
  summary: ReturnType<typeof buildSessionDetailSummary>;
  archivedLabel: string;
}) {
  return (
    <div
      className="mt-3 flex min-w-0 items-center gap-x-3 overflow-x-auto scrollbar-none"
      data-testid="session-header-summary"
    >
      <div className="flex min-w-[8rem] max-w-[50%] shrink items-center gap-2.5">
        <h1 className="min-w-0 truncate font-sans text-xl font-semibold leading-7 tracking-tight text-foreground @min-[640px]:text-2xl @min-[640px]:leading-8">
          {session.title || (
            <ResourceIdButton
              value={session.id}
              className="h-auto min-h-0 px-0 py-0 text-foreground hover:bg-transparent [&_span]:font-sans [&_span]:text-xl [&_span]:font-semibold @min-[640px]:[&_span]:text-2xl"
            />
          )}
        </h1>
        <SessionStatusPill status={session.archived_at ? archivedLabel : summary.statusLabel} />
      </div>
      {summary.chips.map((chip) => (
        <SessionSummaryChip key={chip.key} icon={chip.icon} tooltip={chip.copyValue ? undefined : chip.tooltip}>
          {chip.copyValue ? (
            <ResourceIdButton
              value={chip.copyValue}
              displayValue={chip.key === 'agent' || chip.copyValue.includes(', ') ? chip.value : chip.copyValue}
              className={clsx(
                'h-6 min-h-0 gap-1 rounded-md px-1 py-0 text-foreground/85 hover:bg-muted/60 hover:text-foreground [&_span]:text-xs [&_svg]:size-3 [&_svg]:text-muted-foreground',
                chip.key === 'agent' && '[&_span]:font-sans [&_span]:font-medium',
              )}
            />
          ) : (
            chip.value
          )}
        </SessionSummaryChip>
      ))}
    </div>
  );
}
