import { useI18n } from '../../../shared/i18n';
import { PANEL_TEXT, REASON_TEXT } from './panelText';

type Msg = ReturnType<typeof useI18n>['msg'];

export function usePanelTitle(id: string) {
  const { msg } = useI18n();
  const [title] = PANEL_TEXT[id] ?? [id, ''];
  return msg(`observability.panel.${id}`, title);
}

export function reasonLabel(reason: string, msg: Msg) {
  const text = REASON_TEXT[reason];
  return text ? msg(`observability.reason.${reason === 'max-tokens' ? 'maxTokens' : reason}`, text) : reason;
}
