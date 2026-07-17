import reactHooks from 'eslint-plugin-react-hooks';
import tseslint from 'typescript-eslint';

const productionSourceFiles = ['src/**/*.{ts,tsx}'];
const complexityRule = (max) => ['error', { max, variant: 'modified' }];

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
    },
  },
  ...legacyComplexityBudgets.map(({ files, max }) => ({
    files,
    rules: {
      complexity: complexityRule(max),
    },
  })),
);
