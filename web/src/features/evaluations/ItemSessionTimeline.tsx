import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { toast } from '../../shared/ui/sonner';
import { TooltipProvider } from '../../shared/ui/tooltip';
import { EventsTab } from '../managed-agents/sessions/SessionDetailPage';
import { SessionDetailDeltaFramesContext } from '../managed-agents/sessions/sessionDetailData';
import { sessionDetailEventCopyPayload } from '../managed-agents/sessions/sessionDetailModel';
import { SESSION_MAIN_LANE_ID } from '../managed-agents/sessions/sessionTimeline';
import { EventDetailPanel, SessionTraceEmpty } from '../managed-agents/sessions/SessionTracePanel';
import { copyText } from '../managed-agents/utils';
import { useItemSessionTimeline } from './useItemSessionTimeline';

type Timeline = ReturnType<typeof useItemSessionTimeline>;

function TimelineInspector({ timeline }: { timeline: Timeline }) {
  const { msg } = useI18n();
  const entry = timeline.selectedEntry;
  if (entry && 'traceEntry' in entry) {
    return (
      <EventDetailPanel entry={entry} view="transcript" onClose={() => timeline.selection.setSelectedEntryId(null)} />
    );
  }
  return (
    <SessionTraceEmpty
      message={msg('managedAgents.sessions.detail.selectEvent', 'Select an event to inspect its details.')}
    />
  );
}

export function ItemSessionTimeline({ sessionId, live }: { sessionId: string; live: boolean }) {
  const { msg } = useI18n();
  const timeline = useItemSessionTimeline(sessionId, live);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const { eventData, selection } = timeline;
  const selectEntry = (entryId: string | null) => {
    selection.setSelectedEntryId(entryId);
    if (entryId) setInspectorOpen(true);
  };
  const copyAll = async () => {
    await copyText(sessionDetailEventCopyPayload(timeline.entries));
    toast.success(msg('managedAgents.sessions.detail.copiedFullTranscript', 'Full transcript copied'));
  };
  return (
    <TooltipProvider>
      <SessionDetailDeltaFramesContext.Provider value={eventData.deltaFrames}>
        <EventsTab
          activeLane={timeline.activeLane}
          archivedLaneCount={timeline.archivedLaneCount}
          childLoading={eventData.loading || eventData.childLoading}
          entries={timeline.entries}
          events={eventData.events}
          filteredEntries={timeline.filteredEntries}
          hasFilter={selection.query.trim().length > 0 || timeline.activeLane !== SESSION_MAIN_LANE_ID}
          hoveredEventId={selection.hoveredEventId}
          inspector={<TimelineInspector timeline={timeline} />}
          inspectorOpen={inspectorOpen}
          isMultiAgent={timeline.isMultiAgent}
          lanes={timeline.lanes}
          openModelRequest={timeline.openModelRequest}
          onClearFilters={() => {
            selection.setQuery('');
            timeline.selectLane(SESSION_MAIN_LANE_ID);
          }}
          onCopyAll={() => void copyAll()}
          onCloseInspector={() => setInspectorOpen(false)}
          onHoverEvent={selection.setHoveredEventId}
          onOpenInspector={() => setInspectorOpen(true)}
          onQueryChange={selection.setQuery}
          onSelectEntry={selectEntry}
          onSelectLane={timeline.selectLane}
          onThreadClick={(threadId) =>
            timeline.selectLane(timeline.laneIdByThreadId.get(threadId) ?? SESSION_MAIN_LANE_ID)
          }
          onTimelineSeek={selection.setSelectedEntryId}
          onToggleArchivedLanes={selection.setShowArchivedLanes}
          query={selection.query}
          scrollerRef={timeline.scrollerRef}
          selectedEntry={timeline.selectedEntry}
          selectedEntryId={selection.selectedEntryId}
          sentMessageVersion={0}
          showArchivedLanes={selection.showArchivedLanes}
          suppressScrollSeekUntilRef={timeline.suppressScrollSeekUntilRef}
          threadNameById={timeline.threadNameById}
          timeline={timeline.timeline}
          timelineVisibleIds={timeline.timelineVisibleIds}
          traceStartMs={timeline.traceStartMs}
        />
      </SessionDetailDeltaFramesContext.Provider>
    </TooltipProvider>
  );
}
