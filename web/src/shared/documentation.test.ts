import { expect, test } from 'bun:test';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { documentationHref } from './documentation';

test('console documentation destinations are published in both languages', () => {
  const webRoot = resolve(import.meta.dir, '../..');
  const paths = new Set<string>();
  for (const file of new Bun.Glob('src/**/*.{ts,tsx}').scanSync({ cwd: webRoot })) {
    if (/\.(test|suite|spec|generated)\./.test(file)) continue;
    const source = readFileSync(resolve(webRoot, file), 'utf8');
    for (const match of source.matchAll(/\bdocumentationHref\('([^']+)'/g)) {
      paths.add(match[1]);
    }
  }
  expect(paths.size).toBeGreaterThan(0);
  const navigation = readFileSync(resolve(webRoot, '../docs.json'), 'utf8');
  for (const language of ['zh', 'en']) {
    const spec = JSON.parse(readFileSync(resolve(webRoot, `../openapi/oma.${language}.json`), 'utf8'));
    const operations = Object.entries(spec.paths).flatMap(([path, methods]) =>
      Object.entries(methods as Record<string, { 'x-mint'?: { href?: string } }>).map(([method, operation]) => ({
        page: `${method.toUpperCase()} ${path}`,
        href: operation['x-mint']?.href,
      })),
    );
    for (const path of paths) {
      const destination = `/docs/${language}/${path}`;
      const operation = operations.find((entry) => entry.href === destination);
      const published = operation
        ? navigation.includes(JSON.stringify(operation.page))
        : existsSync(resolve(webRoot, `..${destination}.mdx`)) &&
          navigation.includes(JSON.stringify(destination.slice(1)));
      expect({ destination, published }).toEqual({ destination, published: true });
    }
  }
});

test('documentation links follow the console language', () => {
  expect(documentationHref('api/messages/create-a-message', 'zh-CN')).toBe(
    'https://oma.mintlifysite.com/docs/zh/api/messages/create-a-message',
  );
  expect(documentationHref('api/messages/create-a-message', 'en')).toBe(
    'https://oma.mintlifysite.com/docs/en/api/messages/create-a-message',
  );
});
