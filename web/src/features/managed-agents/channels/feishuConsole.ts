import type { Locale } from '../../../shared/i18n';

const appIdPattern = /^cli_[A-Za-z0-9]+$/;

export function feishuAppConsoleUrl(appId: string | null | undefined, locale: Locale) {
  const id = appId?.trim() ?? '';
  if (!appIdPattern.test(id)) return null;
  const host = locale === 'zh-CN' ? 'open.feishu.cn' : 'open.larksuite.com';
  return `https://${host}/app/${id}/baseinfo`;
}
