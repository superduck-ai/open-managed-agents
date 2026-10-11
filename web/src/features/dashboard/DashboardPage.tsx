import { WorkspaceApiKeysContent } from '../settings/WorkspaceApiKeysPage';
import { WorkspaceWebhooksContent } from '../settings/WorkspaceWebhooksPage';
import { WorkspaceMembersPage } from '../settings/WorkspaceMembersPage';
import { BatchesPage } from './batches';
import {
  ClaudeCodeSettingsPage,
  ClaudeCodeUsagePage,
  ConsoleFeaturePage,
  PlaygroundPage,
  WorkbenchPage,
} from './feature-pages';
import { FilesPage } from './files';
import { DashboardHome } from './home';
import { RetiredConsolePage } from './RetiredConsolePage';
import { currentSkillId } from './model';
import { CreateSkillPage, SkillDetailPage, SkillsPage } from './skills';

type DashboardPageProps = {
  section?: string;
};

export { BatchesPage } from './batches';
export { ConsolePlaceholderPage, MembersPage } from './feature-pages';
export { FilesPage } from './files';
export { CreateSkillPage, SkillDetailPage, SkillsPage } from './skills';

export function DashboardPage({ section = 'dashboard' }: DashboardPageProps) {
  if (section === 'api-keys') {
    return <WorkspaceApiKeysContent />;
  }

  switch (section) {
    case 'dashboard':
      return <DashboardHome />;
    case 'workbench':
      return <WorkbenchPage />;
    case 'playground':
      return <PlaygroundPage />;
    case 'files':
      return <FilesPage />;
    case 'skills':
      return <SkillsPage />;
    case 'skill-new':
      return <CreateSkillPage />;
    case 'skill-detail':
      return <SkillDetailPage skillId={currentSkillId()} />;
    case 'batches':
      return <BatchesPage />;
    case 'members':
      return <WorkspaceMembersPage />;
    case 'limits':
    case 'service-accounts':
    case 'privacy-controls':
    case 'security':
      return <RetiredConsolePage />;
    case 'webhooks':
      return <WorkspaceWebhooksContent />;
    case 'claude-code-usage':
      return <ClaudeCodeUsagePage />;
    case 'claude-code-settings':
      return <ClaudeCodeSettingsPage />;
    default:
      return <ConsoleFeaturePage section={section} />;
  }
}
