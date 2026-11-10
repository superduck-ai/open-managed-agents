const appIdPattern = /^cli_[A-Za-z0-9]+$/;

export function feishuAppConsoleUrl(appId: string | null | undefined) {
  const id = appId?.trim() ?? '';
  if (!appIdPattern.test(id)) return null;
  return `https://open.feishu.cn/app/${id}/baseinfo`;
}
