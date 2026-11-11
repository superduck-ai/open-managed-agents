import { useMemo } from 'react';
import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import java from 'highlight.js/lib/languages/java';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import markdown from 'highlight.js/lib/languages/markdown';
import python from 'highlight.js/lib/languages/python';
import typescript from 'highlight.js/lib/languages/typescript';
import yaml from 'highlight.js/lib/languages/yaml';

const languages = { bash, java, javascript, json, markdown, python, typescript, yaml };
for (const [name, grammar] of Object.entries(languages)) hljs.registerLanguage(name, grammar);

const extensions = new Map<string, keyof typeof languages>([
  ['md', 'markdown'],
  ['markdown', 'markdown'],
  ['py', 'python'],
  ['java', 'java'],
  ['js', 'javascript'],
  ['jsx', 'javascript'],
  ['mjs', 'javascript'],
  ['ts', 'typescript'],
  ['tsx', 'typescript'],
  ['json', 'json'],
  ['yaml', 'yaml'],
  ['yml', 'yaml'],
  ['sh', 'bash'],
  ['bash', 'bash'],
  ['zsh', 'bash'],
]);

export function skillCodeLanguage(path: string) {
  const extension = path.split('.').at(-1)?.toLowerCase() || '';
  const language = extensions.get(extension);
  return { language: language || 'plaintext', label: language ? hljs.getLanguage(language)?.name : undefined };
}

export function SkillCodeBlock({ source, path }: { source: string; path: string }) {
  const { language } = skillCodeLanguage(path);
  const html = useMemo(() => {
    if (language === 'plaintext') return null;
    try {
      const frontmatter =
        language === 'markdown' ? source.match(/^---\r?\n[\s\S]*?\r?\n---(?:\r?\n|$)/)?.[0] : undefined;
      if (frontmatter) {
        return (
          hljs.highlight(frontmatter, { language: 'yaml', ignoreIllegals: true }).value +
          hljs.highlight(source.slice(frontmatter.length), { language, ignoreIllegals: true }).value
        );
      }
      return hljs.highlight(source, { language, ignoreIllegals: true }).value;
    } catch {
      return null;
    }
  }, [source, language]);
  return (
    <pre className="skill-source">
      {html === null ? (
        <code className={`hljs language-${language}`}>{source}</code>
      ) : (
        <code
          className={`hljs language-${language}`}
          dangerouslySetInnerHTML={{ __html: html.replace(/\r/g, '&#13;') }}
        />
      )}
    </pre>
  );
}
