import { memo, useContext } from 'react';
import { useI18n } from '../../../shared/i18n';
import { type DisplayEvent } from '../types';
import { SyntaxCodeBlock } from '../components/CodeBlocks';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { MarkdownTranscriptContent, transcriptContentCode } from './SessionTranscriptContent';
import { sessionEventFamily, sessionEventLabel, sessionLiveDisplayText } from './sessionTraceModel';
import { streamingMarkdownSnapshot } from './streamingMarkdown';

const StableMarkdownTranscript = memo(function StableMarkdownTranscript({ value }: { value: string }) {
  return <MarkdownTranscriptContent value={value} />;
});

export function TranscriptMessageBody({
  displayEvent,
  content,
  streaming,
}: {
  displayEvent: DisplayEvent;
  content: string;
  streaming: boolean;
}) {
  const { msg } = useI18n();
  const deltaFrames = useContext(SessionDetailDeltaFramesContext);
  const liveEvent = deltaFrames[displayEvent.id]?.message ?? displayEvent.event;
  const value = streaming
    ? sessionLiveDisplayText(displayEvent, liveEvent, sessionEventLabel(liveEvent, sessionEventFamily(liveEvent), msg))
    : content;
  const code = streaming ? null : transcriptContentCode(value);
  const snapshot = streaming ? streamingMarkdownSnapshot(value) : { markdown: value, pending: '' };

  return (
    <div data-testid="session-transcript-body" data-streaming={streaming ? 'true' : 'false'}>
      {code ? <SyntaxCodeBlock value={code.value} language={code.language} /> : null}
      {!code && snapshot.markdown ? <StableMarkdownTranscript value={snapshot.markdown} /> : null}
      {!code && snapshot.pending ? <StreamingMarkdownPending value={snapshot.pending} /> : null}
    </div>
  );
}

function StreamingMarkdownPending({ value }: { value: string }) {
  return (
    <span data-testid="session-streaming-markdown-pending" className="whitespace-pre-wrap break-words">
      {value}
    </span>
  );
}
