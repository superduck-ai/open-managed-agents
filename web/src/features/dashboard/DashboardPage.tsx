import { WorkspaceApiKeysContent } from '../settings/WorkspaceApiKeysPage';
import { WorkspaceWebhooksContent } from '../settings/WorkspaceWebhooksPage';
import { OrganizationMembersPage } from '../settings/OrganizationMembersPage';
import { BatchesPage } from './batches';
import {
  ClaudeCodeSettingsPage,
  ClaudeCodeUsagePage,
  ConsoleFeaturePage,
  LimitsPage,
  PlaygroundPage,
  WorkbenchPage
} from './feature-pages';
import { FilesPage } from './files';
import { DashboardHome } from './home';
import { currentSkillId } from './model';
import { PrivacyControlsPage } from './privacy-controls';
import { SecurityPage } from './security';
import { ServiceAccountsPage } from './service-accounts';
import { CreateSkillPage, SkillDetailPage, SkillsPage } from './skills';

type DashboardPageProps = {
  section?: string;
};

export { BatchesPage } from './batches';
export { ConsolePlaceholderPage, LimitsPage, MembersPage } from './feature-pages';
export { FilesPage } from './files';
export { PrivacyControlsPage } from './privacy-controls';
export { SecurityPage } from './security';
export { ServiceAccountsPage } from './service-accounts';
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
    case 'limits':
      return <LimitsPage />;
    case 'members':
      return <OrganizationMembersPage />;
    case 'service-accounts':
      return <ServiceAccountsPage />;
    case 'privacy-controls':
      return <PrivacyControlsPage />;
    case 'security':
      return <SecurityPage />;
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
