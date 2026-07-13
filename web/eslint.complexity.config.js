import reactHooks from 'eslint-plugin-react-hooks';
import tseslint from 'typescript-eslint';

const productionSourceFiles = ['src/**/*.{ts,tsx}'];
const complexityRule = (max) => ['error', { max, variant: 'modified' }];
const fileLineRule = (max) => ['error', { max, skipBlankLines: true, skipComments: true }];
const functionLineRule = (max) => ['error', { max, skipBlankLines: true, skipComments: true }];

// Existing hotspots use their measured file maximum as a ratchet. New files,
// and files not listed here, must stay at or below the default budget of 20.
const legacyComplexityBudgets = [
  {
    max: 21,
    files: ['src/features/managed-agents/sessions/sessionTraceRows.tsx', 'src/features/workbench/model.ts'],
  },
  {
    max: 23,
    files: ['src/app/layout/ConsoleLayout.tsx'],
  },
  {
    max: 24,
    files: ['src/features/managed-agents/agents/tools/AgentToolsSection.tsx', 'src/features/managed-agents/api.ts'],
  },
  {
    max: 26,
    files: ['src/features/settings/OrganizationMembersPage.tsx'],
  },
  {
    max: 27,
    files: [
      'src/features/managed-agents/sessions/SessionDetailPage.tsx',
      'src/features/settings/OrganizationSettingsPage.tsx',
    ],
  },
  {
    max: 28,
    files: [
      'src/features/managed-agents/agents/detail.tsx',
      'src/features/settings/WorkloadIdentitySettingsPage.tsx',
      'src/features/workbench/editor.tsx',
    ],
  },
  {
    max: 29,
    files: ['src/features/managed-agents/sessions/sessionTraceModel.ts'],
  },
  {
    max: 31,
    files: ['src/features/managed-agents/quickstart/AgentQuickstartPage.tsx'],
  },
  {
    max: 32,
    files: ['src/features/managed-agents/sessions/sessionDetailModel.ts'],
  },
  {
    max: 33,
    files: ['src/features/settings/AdminKeysSettingsPage.tsx'],
  },
  {
    max: 36,
    files: ['src/features/managed-agents/resources/entities.tsx'],
  },
  {
    max: 37,
    files: ['src/features/workbench/dialogs.tsx'],
  },
  {
    max: 43,
    files: ['src/features/managed-agents/resources/dialogs.tsx'],
  },
  {
    max: 46,
    files: ['src/features/managed-agents/resources/detail.tsx'],
  },
  {
    max: 48,
    files: ['src/features/managed-agents/agents/AgentsResourcePage.tsx'],
  },
  {
    max: 78,
    files: ['src/features/workbench/WorkbenchPage.tsx'],
  },
];

// File and function budgets use the same ratchet approach. Files not listed
// here use 500 effective lines per file and 200 per function. Each exception
// records the current measured maximum and must only move downward.
const legacyMaintainabilityBudgets = [
  { file: 'src/app/layout/ConsoleLayout.tsx', maxLines: 929 },
  { file: 'src/features/analytics/AnalyticsPages.tsx', maxLines: 893 },
  { file: 'src/features/dashboard/privacy-controls.tsx', maxFunctionLines: 266 },
  { file: 'src/features/dashboard/security.tsx', maxFunctionLines: 271 },
  { file: 'src/features/dashboard/service-accounts.tsx', maxFunctionLines: 234 },
  { file: 'src/features/dashboard/skills.tsx', maxLines: 1115, maxFunctionLines: 231 },
  { file: 'src/features/managed-agents/agentConfig.ts', maxLines: 810 },
  {
    file: 'src/features/managed-agents/agents/AgentsResourcePage.tsx',
    maxLines: 752,
    maxFunctionLines: 666,
  },
  { file: 'src/features/managed-agents/agents/create-dialog.tsx', maxFunctionLines: 369 },
  {
    file: 'src/features/managed-agents/agents/detail.tsx',
    maxLines: 2100,
    maxFunctionLines: 370,
  },
  { file: 'src/features/managed-agents/api.ts', maxLines: 1588 },
  { file: 'src/features/managed-agents/components/common.tsx', maxLines: 1111 },
  {
    file: 'src/features/managed-agents/quickstart/AgentQuickstartPage.tsx',
    maxLines: 1178,
    maxFunctionLines: 1098,
  },
  {
    file: 'src/features/managed-agents/quickstart/components.tsx',
    maxLines: 2708,
    maxFunctionLines: 287,
  },
  {
    file: 'src/features/managed-agents/resources/detail.tsx',
    maxLines: 1850,
    maxFunctionLines: 574,
  },
  {
    file: 'src/features/managed-agents/resources/dialogs.tsx',
    maxLines: 651,
    maxFunctionLines: 430,
  },
  {
    file: 'src/features/managed-agents/resources/entities.tsx',
    maxLines: 959,
    maxFunctionLines: 745,
  },
  { file: 'src/features/managed-agents/resources/model.tsx', maxLines: 615 },
  {
    file: 'src/features/managed-agents/sessions/SessionDetailPage.tsx',
    maxLines: 890,
    maxFunctionLines: 626,
  },
  { file: 'src/features/managed-agents/sessions/SessionTracePanel.tsx', maxLines: 1728 },
  { file: 'src/features/managed-agents/sessions/sessionDetailModel.ts', maxLines: 948 },
  {
    file: 'src/features/managed-agents/sessions/sessionTimeline.tsx',
    maxLines: 1295,
    maxFunctionLines: 430,
  },
  { file: 'src/features/managed-agents/sessions/sessionTraceModel.ts', maxLines: 2368 },
  { file: 'src/features/managed-agents/sessions/sessionTraceRows.tsx', maxLines: 622 },
  { file: 'src/features/managed-agents/types.ts', maxLines: 685 },
  {
    file: 'src/features/settings/AdminKeysSettingsPage.tsx',
    maxLines: 633,
    maxFunctionLines: 405,
  },
  { file: 'src/features/settings/BillingSettingsPage.tsx', maxFunctionLines: 348 },
  {
    file: 'src/features/settings/IdentityAndAccessSettingsPage.tsx',
    maxFunctionLines: 422,
  },
  {
    file: 'src/features/settings/OrganizationMembersPage.tsx',
    maxLines: 729,
    maxFunctionLines: 295,
  },
  {
    file: 'src/features/settings/OrganizationSettingsPage.tsx',
    maxLines: 521,
    maxFunctionLines: 285,
  },
  {
    file: 'src/features/settings/WorkloadIdentitySettingsPage.tsx',
    maxLines: 768,
    maxFunctionLines: 477,
  },
  { file: 'src/features/settings/WorkspaceApiKeysPage.tsx', maxLines: 752 },
  {
    file: 'src/features/settings/WorkspaceWebhooksPage.tsx',
    maxLines: 1363,
    maxFunctionLines: 218,
  },
  {
    file: 'src/features/workbench/WorkbenchPage.tsx',
    maxLines: 2682,
    maxFunctionLines: 2513,
  },
  { file: 'src/features/workbench/dialogs.tsx', maxLines: 714 },
  {
    file: 'src/features/workbench/drawers.tsx',
    maxLines: 1298,
    maxFunctionLines: 379,
  },
  {
    file: 'src/features/workbench/editor.tsx',
    maxLines: 723,
    maxFunctionLines: 349,
  },
  {
    file: 'src/features/workbench/evaluate.tsx',
    maxLines: 887,
    maxFunctionLines: 475,
  },
  { file: 'src/features/workbench/model.ts', maxLines: 1575 },
  { file: 'src/shared/api/anthropic.ts', maxLines: 572 },
  { file: 'src/shared/ui/sidebar.tsx', maxLines: 519 },
];

export default tseslint.config(
  {
    ignores: [
      'dist',
      'src/**/*.test.{ts,tsx}',
      'src/**/*.suite.{ts,tsx}',
      'src/**/*.test-utils.{ts,tsx}',
      'src/features/managed-agents/quickstart/platformQuickstartOfficialRequest.generated.ts',
    ],
  },
  {
    files: productionSourceFiles,
    linterOptions: {
      reportUnusedDisableDirectives: 'off',
    },
    languageOptions: {
      parser: tseslint.parser,
    },
    plugins: {
      'react-hooks': reactHooks,
    },
    rules: {
      complexity: complexityRule(20),
      'max-lines': fileLineRule(500),
      'max-lines-per-function': functionLineRule(200),
    },
  },
  ...legacyComplexityBudgets.map(({ files, max }) => ({
    files,
    rules: {
      complexity: complexityRule(max),
    },
  })),
  ...legacyMaintainabilityBudgets.map(({ file, maxLines, maxFunctionLines }) => {
    const rules = {};
    if (maxLines !== undefined) {
      rules['max-lines'] = fileLineRule(maxLines);
    }
    if (maxFunctionLines !== undefined) {
      rules['max-lines-per-function'] = functionLineRule(maxFunctionLines);
    }
    return { files: [file], rules };
  }),
);
