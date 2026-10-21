import { expect, test } from 'bun:test';
import './setup';

test('DOM diagnostics stay bounded without changing node identity or content', () => {
  const node = document.createElement('div');
  node.textContent = 'x'.repeat(4096);
  expect(Bun.inspect(node)).toBe('<div>');
  expect(node.textContent).toHaveLength(4096);
  expect(node instanceof HTMLElement).toBe(true);
  expect(Bun.inspect(document)).toBe('<#document>');
});
