import { z } from 'zod';
import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import {
  sessionEventType,
  sessionEventTranscriptText,
  sessionEventContentBlocks,
  sessionStatusFromEvents,
} from '../sessions/sessionTraceModel';

const bindingSchema = z.object({ id: z.string(), version: z.number() });
const stopReasonSchema = z.object({ type: z.string() });
const textBlockSchema = z.object({
  type: z.literal('text'),
  text: z.string().trim().min(1),
});

function hasReplyText(event: QuickstartSessionEvent) {
  if (!['agent.message', 'assistant.message'].includes(sessionEventType(event))) return false;
  const blocks = sessionEventContentBlocks(event);
  return blocks.length
    ? blocks.some((block) => textBlockSchema.safeParse(block).success)
    : Boolean(sessionEventTranscriptText(event));
}

export function sessionMatchesQuickstart(session: SessionApiResponse | undefined, binding: string) {
  if (!session) return false;
  const agent = bindingSchema.safeParse(session.agent);
  return agent.success && `${agent.data.id}:${agent.data.version}:${session.environment_id}` === binding;
}

export function quickstartConversationState(
  events: QuickstartSessionEvent[],
  savedStatus: string,
  awaitingTool: boolean,
) {
  const types = events.map(sessionEventType);
  const lastUserIndex = types.lastIndexOf('user.message');
  const lastInputIndex = Math.max(
    lastUserIndex,
    types.lastIndexOf('user.tool_confirmation'),
    types.lastIndexOf('user.custom_tool_result'),
  );
  const latest = sessionStatusFromEvents(events.slice(lastInputIndex + 1));
  const executionStarted =
    types.lastIndexOf('span.model_request_start') >
    Math.max(lastInputIndex, types.lastIndexOf('session.status_rescheduled'));
  const terminated = ['terminated', 'deleted'].includes(savedStatus);
  const status = terminated ? savedStatus : (latest?.status ?? savedStatus);
  const reason = stopReasonSchema.safeParse(latest?.event.stop_reason);
  const hasReply = events.slice(lastUserIndex + 1).some(hasReplyText);
  const interrupted = events.slice(lastUserIndex + 1).some((event) => sessionEventType(event) === 'user.interrupt');
  const running =
    !terminated && (status === 'running' || status === 'rescheduling' || (lastInputIndex >= 0 && !latest));
  return {
    status,
    complete: completedTurn({
      lastUserIndex,
      hasReply,
      interrupted,
      status,
      latestStatus: latest?.status,
      awaitingTool,
      reason: reason.success ? reason.data.type : undefined,
    }),
    running,
    waiting: running && !awaitingTool && !executionStarted && (!latest || latest.status === 'rescheduling'),
    failedTurn: reason.success && ['error', 'retries_exhausted'].includes(reason.data.type),
    stopped: terminated || (interrupted && status === 'idle'),
  };
}

function completedTurn({
  lastUserIndex,
  hasReply,
  interrupted,
  status,
  latestStatus,
  awaitingTool,
  reason,
}: {
  lastUserIndex: number;
  hasReply: boolean;
  interrupted: boolean;
  status: string;
  latestStatus?: string;
  awaitingTool: boolean;
  reason?: string;
}) {
  return (
    lastUserIndex >= 0 &&
    hasReply &&
    !interrupted &&
    status === 'idle' &&
    latestStatus === 'idle' &&
    !awaitingTool &&
    (!reason || reason === 'end_turn')
  );
}
