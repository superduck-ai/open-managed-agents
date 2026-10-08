import { quickstartConversationState, sessionMatchesQuickstart } from './conversationModel';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import {
  interruptQuickstartSession,
  postQuickstartSessionMessage,
  postSessionToolConfirmation,
  retrieveSessionDetailSession,
} from '../api';
import type { SessionToolConfirmationInput, SessionThreadApiResponse } from '../types';
import { errorMessage } from '../utils';
import { findActiveAwaitingToolCall } from '../sessions/sessionDetailModel';
import { useSessionDetailEventData } from '../sessions/sessionDetailData';
import {
  buildSessionEventEntries,
  latestRequiresActionEventIDs,
  latestOpenModelRequest,
  sessionEventType,
} from '../sessions/sessionTraceModel';
import { quickstartCopy } from './copy';
import type { QuickstartWizard } from './useQuickstartWizard';

const noThreads: SessionThreadApiResponse[] = [];

export function useQuickstartConversation(
  wizard: QuickstartWizard,
  workspaceID: string,
  accountID: string,
  binding: string,
) {
  const { locale, msg } = useI18n();
  const text = quickstartCopy(locale);
  const sessionID = wizard.progress.sessionBinding === binding ? wizard.progress.sessionID : '';
  const session = useQuery({
    queryKey: ['quickstart-session', accountID, workspaceID, sessionID],
    queryFn: () => retrieveSessionDetailSession(sessionID, workspaceID),
    enabled: Boolean(sessionID),
    retry: false,
    staleTime: 1000,
    refetchInterval: 2000,
  });
  const bindingValid = sessionMatchesQuickstart(session.data, binding);
  const [refreshKey, setRefreshKey] = useState(0);
  const data = useSessionDetailEventData({
    sessionId: sessionID || null,
    workspaceId: workspaceID,
    threads: noThreads,
    includeArchivedThreads: false,
    live: bindingValid && !['terminated', 'deleted'].includes(session.data?.status ?? ''),
    refreshKey,
  });
  const latestData = useRef(data);
  latestData.current = data;
  const [message, setMessage] = useState(text.suggested);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sentVersion, setSentVersion] = useState(0);
  const mounted = useRef(true);
  const lock = useRef(false);
  const connected = useRef({ id: sessionID, ready: data.connected });
  connected.current = { id: sessionID, ready: data.connected };
  const waiter = useRef<{ id: string; resolve: () => void; reject: (cause: Error) => void } | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      waiter.current?.reject(new Error('Conversation closed'));
      waiter.current = null;
    };
  }, []);
  useEffect(() => {
    if (data.connected && waiter.current?.id === sessionID) waiter.current.resolve();
  }, [data.connected, sessionID]);
  const awaitConnection = (id: string) => {
    if (connected.current.id === id && connected.current.ready) return Promise.resolve();
    return new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        waiter.current = null;
        reject(new Error(text.connectionTimeout));
      }, 30_000);
      waiter.current = {
        id,
        resolve: () => {
          window.clearTimeout(timer);
          waiter.current = null;
          resolve();
        },
        reject: (cause) => {
          window.clearTimeout(timer);
          waiter.current = null;
          reject(cause);
        },
      };
    });
  };
  const entries = useMemo(
    () =>
      buildSessionEventEntries(
        data.events.filter((event) => sessionEventType(event) !== 'user.interrupt'),
        'transcript',
        0,
        msg,
        { platformTranscriptFiltering: true },
      ),
    [data.events, msg],
  );
  const action = useMemo(
    () => findActiveAwaitingToolCall(entries, latestRequiresActionEventIDs(data.events)),
    [data.events, entries],
  );
  const { status, complete, running, failedTurn, stopped } = quickstartConversationState(
    data.events,
    session.data?.status ?? 'idle',
    Boolean(action),
  );
  const disabled =
    Boolean(sessionID && (!bindingValid || ['terminated', 'deleted'].includes(status))) ||
    wizard.busy ||
    Boolean(wizard.progress.pending);
  const unconfirmed = wizard.progress.sessionBinding === binding && Boolean(wizard.progress.unconfirmedMessage);
  const submitMessage = async (id: string, body: string) => {
    wizard.update((value) => ({ ...value, unconfirmedMessage: body }));
    try {
      const response = await postQuickstartSessionMessage(id, body, workspaceID);
      if (!mounted.current) return;
      latestData.current.appendPrimaryEvents(response.data ?? []);
      wizard.update((value) => ({ ...value, unconfirmedMessage: '' }));
      setMessage('');
      setSentVersion((value) => value + 1);
    } catch (cause) {
      const status = typeof cause === 'object' && cause && 'status' in cause ? Number(cause.status) : 0;
      if (status >= 400 && status < 500) wizard.update((value) => ({ ...value, unconfirmedMessage: '' }));
      throw cause;
    }
  };
  const send = async () => {
    const body = message.trim();
    if (!body || lock.current || running || action || disabled || unconfirmed) return;
    lock.current = true;
    setSending(true);
    setError(null);
    try {
      const saved = sessionID ? session.data : await wizard.createSession();
      if (!saved || !mounted.current) return;
      await awaitConnection(saved.id);
      if (!mounted.current) return;
      await submitMessage(saved.id, body);
    } catch (cause) {
      if (mounted.current) setError(errorMessage(cause));
    } finally {
      lock.current = false;
      if (mounted.current) setSending(false);
    }
  };
  const confirm = async (input: SessionToolConfirmationInput) => {
    if (lock.current || !sessionID || disabled || unconfirmed) return;
    lock.current = true;
    setError(null);
    try {
      const response = await postSessionToolConfirmation(sessionID, input, workspaceID);
      if (mounted.current) data.appendPrimaryEvents(response.data ?? []);
    } catch (cause) {
      if (mounted.current) setError(errorMessage(cause));
    } finally {
      lock.current = false;
    }
  };
  const stop = async () => {
    if (lock.current || !sessionID) return;
    lock.current = true;
    setSending(true);
    setError(null);
    try {
      await interruptQuickstartSession(sessionID, workspaceID);
      if (mounted.current) await session.refetch();
    } catch (cause) {
      if (mounted.current) setError(errorMessage(cause));
    } finally {
      lock.current = false;
      if (mounted.current) setSending(false);
    }
  };
  const refresh = async () => {
    setRefreshKey((value) => value + 1);
    await session.refetch();
  };
  const newConversation = () => {
    if (sending || running || action || wizard.busy || wizard.progress.pending || unconfirmed) return;
    wizard.update((value) => ({ ...value, sessionID: '', sessionBinding: '', unconfirmedMessage: '' }));
    setError(null);
    setMessage(text.suggested);
  };
  return {
    sessionID,
    session,
    data,
    entries,
    action,
    complete,
    stopped,
    running,
    disabled,
    unconfirmed,
    message,
    setMessage,
    sending,
    error,
    sentVersion,
    send,
    confirm,
    stop,
    refresh,
    newConversation,
    failedTurn,
    bindingValid,
    openModelRequest: latestOpenModelRequest(data.events),
  };
}
