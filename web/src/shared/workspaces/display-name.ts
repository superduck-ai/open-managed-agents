import type { MessageValues } from '../i18n/context';

type Translate = (id: string, defaultMessage: string, values?: MessageValues) => string;

const defaultWorkspaceToken = 'Default';

export function localizedWorkspaceName(name: string, msg: Translate) {
  if (name === defaultWorkspaceToken) {
    return msg('workspace.defaultName', 'Default');
  }
  return name;
}
