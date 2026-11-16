import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { cn } from '../../shared/lib/utils';
import { Button } from '../../shared/ui/button';
import { Empty, EmptyHeader, EmptyTitle } from '../../shared/ui/empty';
import { ScrollArea } from '../../shared/ui/scroll-area';
import type { SessionTurn } from './api';
import { mediaDetail } from './caseInputs';

export function splitSessionTurns(turns: SessionTurn[], chosen: ReadonlySet<number>) {
  return {
    available: turns.filter((turn) => !chosen.has(turn.index)),
    chosenTurns: turns.filter((turn) => chosen.has(turn.index)),
  };
}

export function moveSessionTurns(chosen: ReadonlySet<number>, indexes: number[], intoChosen: boolean) {
  const next = new Set(chosen);
  for (const index of indexes) {
    if (intoChosen) next.add(index);
    else next.delete(index);
  }
  return next;
}

export function SessionTurnTransfer({
  turns,
  chosen,
  onChosenChange,
  status,
  error,
}: {
  turns: SessionTurn[];
  chosen: ReadonlySet<number>;
  onChosenChange: (chosen: Set<number>) => void;
  status: 'idle' | 'loading' | 'error' | 'empty' | 'ready';
  error: string;
}) {
  const { msg } = useI18n();
  const [marked, setMarked] = useState<Set<number>>(new Set());
  const { available, chosenTurns } = splitSessionTurns(turns, chosen);
  const markedAvailable = available.filter((turn) => marked.has(turn.index)).map((turn) => turn.index);
  const markedChosen = chosenTurns.filter((turn) => marked.has(turn.index)).map((turn) => turn.index);

  function move(indexes: number[], intoChosen: boolean) {
    if (indexes.length === 0) return;
    onChosenChange(moveSessionTurns(chosen, indexes, intoChosen));
    setMarked((current) => {
      const next = new Set(current);
      for (const index of indexes) next.delete(index);
      return next;
    });
  }

  function toggle(index: number) {
    setMarked((current) => {
      const next = new Set(current);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  }

  return (
    <div className="flex min-h-0 flex-col gap-3 md:flex-row md:items-stretch">
      <TurnPane
        title={msg('evaluations.availableTurns', 'Available turns')}
        count={status === 'ready' ? available.length : 0}
        turns={available}
        marked={marked}
        empty={paneEmpty(status, error, 'available', msg)}
        onToggle={toggle}
        onMove={(index) => move([index], true)}
      />
      <div className="flex flex-row items-center justify-center gap-2 md:flex-col">
        <MoveButton
          label={msg('evaluations.moveRight', 'Move in')}
          disabled={markedAvailable.length === 0}
          onClick={() => move(markedAvailable, true)}
        >
          <ChevronRight className="rotate-90 md:rotate-0" />
        </MoveButton>
        <MoveButton
          label={msg('evaluations.moveLeft', 'Move out')}
          disabled={markedChosen.length === 0}
          onClick={() => move(markedChosen, false)}
        >
          <ChevronLeft className="rotate-90 md:rotate-0" />
        </MoveButton>
        <MoveButton
          label={msg('evaluations.moveAllRight', 'Move all in')}
          disabled={available.length === 0}
          onClick={() =>
            move(
              available.map((turn) => turn.index),
              true,
            )
          }
        >
          <ChevronsRight className="rotate-90 md:rotate-0" />
        </MoveButton>
        <MoveButton
          label={msg('evaluations.moveAllLeft', 'Move all out')}
          disabled={chosenTurns.length === 0}
          onClick={() =>
            move(
              chosenTurns.map((turn) => turn.index),
              false,
            )
          }
        >
          <ChevronsLeft className="rotate-90 md:rotate-0" />
        </MoveButton>
      </div>
      <TurnPane
        title={msg('evaluations.chosenTurns', 'To import')}
        count={chosenTurns.length}
        turns={chosenTurns}
        marked={marked}
        empty={paneEmpty(status, error, 'chosen', msg)}
        onToggle={toggle}
        onMove={(index) => move([index], false)}
      />
    </div>
  );
}

function MoveButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <Button type="button" variant="outline" size="icon" aria-label={label} disabled={disabled} onClick={onClick}>
      {children}
    </Button>
  );
}

function TurnPane({
  title,
  count,
  turns,
  marked,
  empty,
  onToggle,
  onMove,
}: {
  title: string;
  count: number;
  turns: SessionTurn[];
  marked: ReadonlySet<number>;
  empty: string;
  onToggle: (index: number) => void;
  onMove: (index: number) => void;
}) {
  const { msg } = useI18n();
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="text-sm font-medium">{title}</h3>
        <span className="text-xs text-muted-foreground tabular-nums">{count}</span>
      </div>
      <div className="h-80 overflow-hidden rounded-lg border border-border">
        {turns.length === 0 ? (
          <Empty className="h-full rounded-none border-0">
            <EmptyHeader>
              <EmptyTitle>{empty}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <ScrollArea className="h-full [&_[data-slot=scroll-area-content]]:!min-w-0 [&_[data-slot=scroll-area-content]]:!w-full">
            <ul>
              {turns.map((turn) => (
                <li key={turn.index} className="border-b border-border last:border-b-0">
                  <button
                    type="button"
                    aria-pressed={marked.has(turn.index)}
                    className={cn(
                      'flex w-full gap-3 px-3 py-3 text-left hover:bg-muted/60',
                      marked.has(turn.index) && 'bg-accent',
                    )}
                    onClick={() => onToggle(turn.index)}
                    onDoubleClick={() => onMove(turn.index)}
                  >
                    <span className="grid min-w-0 gap-2">
                      <span className="text-xs text-muted-foreground tabular-nums">
                        {msg('evaluations.turnIndex', 'Turn {index}', { index: turn.index })}
                      </span>
                      <span>
                        <span className="text-xs font-medium">{msg('evaluations.userMessage', 'User')}</span>
                        <TurnQuery turn={turn} />
                      </span>
                      <span className="rounded-md bg-muted px-2 py-1.5">
                        <span className="text-xs font-medium text-muted-foreground">
                          {msg('evaluations.assistantMessage', 'Assistant')}
                        </span>
                        <span className="mt-0.5 block line-clamp-3 text-sm text-muted-foreground [overflow-wrap:anywhere]">
                          {turn.answer || msg('evaluations.turnNoReply', 'No reply in this turn')}
                        </span>
                      </span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </div>
    </section>
  );
}

function TurnQuery({ turn }: { turn: SessionTurn }) {
  const { msg } = useI18n();
  const detail = mediaDetail(turn.inputs, {
    images: (count) => msg('evaluations.imageCount', '{count} images', { count }),
    documents: (count) => msg('evaluations.documentCount', '{count} documents', { count }),
  });
  return (
    <>
      <span className="mt-0.5 block line-clamp-3 text-sm [overflow-wrap:anywhere]">
        {turn.query || detail || msg('evaluations.imageOnly', 'Image')}
      </span>
      {turn.query && detail ? <span className="mt-1 block text-xs text-muted-foreground">{detail}</span> : null}
    </>
  );
}

function paneEmpty(
  status: 'idle' | 'loading' | 'error' | 'empty' | 'ready',
  error: string,
  side: 'available' | 'chosen',
  msg: (id: string, fallback: string) => string,
) {
  if (side === 'chosen') return msg('evaluations.chosenEmpty', 'Turns on this side become cases.');
  if (status === 'loading') return msg('evaluations.loading', 'Loading');
  if (status === 'error') return error;
  if (status === 'idle') return msg('evaluations.chooseSessionFirst', 'Choose a session first.');
  if (status === 'empty') return msg('evaluations.noTurns', 'This session has no user messages');
  return msg('evaluations.turnsAllChosen', 'Every turn is on the right.');
}
