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
    <div className="mt-3 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5">
      <div className="flex min-w-0 max-w-full items-center gap-2">
        <h1 className="min-w-0 truncate font-serif text-[28px] font-medium leading-[1.3] tracking-[-0.015em] text-foreground">
          {session.title || <ResourceIdButton value={session.id} className="[&_span]:text-[28px]" />}
        </h1>
        <SessionStatusPill status={session.archived_at ? archivedLabel : summary.statusLabel} />
      </div>
      {summary.chips.map((chip) => (
        <SessionSummaryChip key={chip.key} icon={chip.icon} tooltip={chip.copyValue ? undefined : chip.tooltip}>
          {chip.copyValue ? (
            <ResourceIdButton
              value={chip.copyValue}
              displayValue={chip.key === 'agent' || chip.copyValue.includes(', ') ? chip.value : chip.copyValue}
            />
          ) : (
            chip.value
          )}
        </SessionSummaryChip>
      ))}
    </div>
  );
}
