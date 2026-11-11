import { useEffect, useState } from 'react';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { useI18n } from '../../../shared/i18n';
import { errorMessage } from '../utils';
import type { ChannelAuthorization, ChannelQr } from './api';

export function useChannelAuthorization(
  workspaceId: string,
  enabled: boolean,
  onBound: (appId: string) => void,
  onError: (message: string) => void,
) {
  const { msg } = useI18n();
  const [authorization, setAuthorization] = useState<ChannelAuthorization | null>(null);
  const [qr, setQr] = useState<ChannelQr | null>(null);
  const [pollError, setPollError] = useState('');
  const [showQr, setShowQr] = useState(false);
  const [authorizing, setAuthorizing] = useState(false);
  const unknownMessage = msg(
    'managedAgents.channels.authorizationUnknown',
    '授权结果待核对，请在飞书后台确认应用后使用手动输入',
  );
  useEffect(() => {
    if (!enabled || !authorization || authorization.status !== 'pending') return;
    const controller = new AbortController();
    let inFlight = false;
    const timer = window.setInterval(() => {
      if (inFlight) return;
      inFlight = true;
      void anthropicBetaApi.channels
        .authorization<ChannelAuthorization>(workspaceId, authorization.id)
        .then((next) => {
          if (controller.signal.aborted) return;
          setAuthorization(next);
          setQr(next);
          setPollError(next.status === 'unknown' ? unknownMessage : '');
          if (next.status === 'bound') onBound(next.app_id ?? '');
        })
        .catch((err) => {
          if (!controller.signal.aborted) setPollError(errorMessage(err));
        })
        .finally(() => {
          inFlight = false;
        });
    }, 2500);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [authorization?.id, authorization?.status, workspaceId, onBound, enabled, unknownMessage]);

  const authorize = async (name: string, openWindow: boolean) => {
    if (!name.trim()) {
      onError(msg('managedAgents.channels.nameRequired', '请输入机器人名称'));
      return;
    }
    if (authorization?.status === 'unknown') {
      onError(unknownMessage);
      return;
    }
    setShowQr(!openWindow);
    const popup = openWindow ? window.open('about:blank', '_blank') : null;
    if (popup) {
      popup.opener = null;
      popup.document.title = msg('managedAgents.channels.openingAuthorization', '正在打开飞书授权…');
      popup.document.body.textContent = popup.document.title;
      popup.document.body.style.cssText =
        'margin:0;display:grid;min-height:100vh;place-items:center;font:14px system-ui;color:#666';
    }
    setAuthorizing(true);
    setPollError('');
    onError('');
    try {
      const next =
        authorization?.status === 'pending'
          ? authorization
          : await anthropicBetaApi.channels.authorization<ChannelAuthorization>(workspaceId, undefined, {
              name: name.trim(),
            });
      setAuthorization(next);
      setQr(next);
      if (popup && !popup.closed) popup.location.replace(next.url);
    } catch (error) {
      popup?.close();
      onError(errorMessage(error));
    } finally {
      setAuthorizing(false);
    }
  };
  return { authorization, qr, setQr, showQr, authorizing, authorize, pollError: enabled ? pollError : '' };
}
