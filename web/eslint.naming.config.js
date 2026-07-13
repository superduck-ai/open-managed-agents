import reactHooks from 'eslint-plugin-react-hooks';
import tseslint from 'typescript-eslint';

const namingConvention = [
  {
    selector: 'variable',
    modifiers: ['destructured'],
    format: null,
  },
  {
    selector: 'variable',
    format: ['camelCase', 'PascalCase', 'UPPER_CASE'],
    leadingUnderscore: 'allow',
    trailingUnderscore: 'allow',
  },
  {
    selector: 'function',
    format: ['camelCase', 'PascalCase'],
    leadingUnderscore: 'allow',
  },
  {
    selector: 'parameter',
    modifiers: ['destructured'],
    format: null,
  },
  {
    selector: 'parameter',
    format: ['camelCase', 'PascalCase'],
    leadingUnderscore: 'allow',
  },
  {
    selector: 'typeLike',
    format: ['PascalCase'],
  },
  {
    selector: 'typeParameter',
    format: ['PascalCase'],
  },
  {
    selector: ['classMethod', 'classProperty'],
    format: ['camelCase'],
    leadingUnderscore: 'allow',
  },
];

export default tseslint.config(
  {
    ignores: ['dist', 'src/features/managed-agents/quickstart/platformQuickstartOfficialRequest.generated.ts'],
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    linterOptions: {
      reportUnusedDisableDirectives: 'off',
    },
    languageOptions: {
      parser: tseslint.parser,
    },
    plugins: {
      '@typescript-eslint': tseslint.plugin,
      'react-hooks': reactHooks,
    },
    rules: {
      '@typescript-eslint/naming-convention': ['error', ...namingConvention],
    },
  },
);
