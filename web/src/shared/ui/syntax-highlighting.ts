import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import python from 'highlight.js/lib/languages/python';
import typescript from 'highlight.js/lib/languages/typescript';
import yamlLanguage from 'highlight.js/lib/languages/yaml';

export type HighlightLanguage =
  'bash' | 'bash-yaml' | 'javascript' | 'json' | 'plaintext' | 'python' | 'typescript' | 'yaml';

hljs.registerLanguage('bash', bash);
hljs.registerLanguage('shell', bash);
hljs.registerLanguage('javascript', javascript);
hljs.registerLanguage('json', json);
hljs.registerLanguage('python', python);
hljs.registerLanguage('typescript', typescript);
hljs.registerLanguage('yaml', yamlLanguage);

export function normalizeHighlightLanguage(language: string | undefined, value: string): HighlightLanguage {
  const normalized = language?.toLowerCase();
  if (normalized === 'yaml' || normalized === 'yml') return 'yaml';
  if (normalized === 'json') return 'json';
  if (['bash', 'shell', 'sh', 'zsh', 'cli', 'curl'].includes(normalized ?? '')) return 'bash';
  if (normalized === 'py' || normalized === 'python') return 'python';
  if (normalized === 'ts' || normalized === 'tsx' || normalized === 'typescript') return 'typescript';
  if (normalized === 'js' || normalized === 'jsx' || normalized === 'javascript') return 'javascript';
  if (normalized) return 'plaintext';
  return looksLikeJSON(value) ? 'json' : 'plaintext';
}

export function highlightCodeHTML(code: string, language: HighlightLanguage): string {
  if (language === 'plaintext') return escapeHTML(code);
  if (language === 'bash-yaml') return highlightBashYAMLCommand(code);
  return highlightRegisteredLanguage(code, language);
}

export function highlightBashYAMLCommand(code: string): string {
  const heredocStart = code.indexOf('<<YAML\n');
  if (heredocStart < 0) return highlightRegisteredLanguage(code, 'bash');

  const bodyStart = heredocStart + '<<YAML\n'.length;
  const beforeYAML = code.slice(0, bodyStart);
  const rest = code.slice(bodyStart);
  const closingMatch = rest.match(/([\s\S]*?)(\nYAML)$/);
  const yamlBody = closingMatch ? (closingMatch[1] ?? '') : rest;
  const closingYAML = closingMatch?.[2] ?? '';

  return [
    highlightRegisteredLanguage(beforeYAML, 'bash'),
    highlightRegisteredLanguage(yamlBody, 'yaml'),
    closingYAML ? highlightRegisteredLanguage(closingYAML, 'bash') : '',
  ].join('');
}

export function highlightRegisteredLanguage(
  code: string,
  language: Exclude<HighlightLanguage, 'bash-yaml' | 'plaintext'>,
): string {
  if (!hljs.getLanguage(language)) return escapeHTML(code);
  try {
    return hljs.highlight(code, { language, ignoreIllegals: true }).value;
  } catch {
    return escapeHTML(code);
  }
}

export function escapeHTML(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function looksLikeJSON(value: string) {
  const trimmed = value.trim();
  return (trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'));
}
