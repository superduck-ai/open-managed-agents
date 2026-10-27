import { expect, test } from 'bun:test';
import { providerBaseURL, providerRequestURL } from './provider-form';

test('normalizes full request URLs and preserves gateway prefixes', () => {
  for (const suffix of ['/v1/messages', '/v1/chat/completions/', '/v1', '']) {
    const input = ` https://api.deepseek.com/anthropic${suffix} `;
    expect(providerBaseURL(input)).toBe('https://api.deepseek.com/anthropic');
    expect(providerRequestURL(input, 'messages')).toBe('https://api.deepseek.com/anthropic/v1/messages');
  }
  expect(providerRequestURL('https://gateway.test/team/v1/chat/completions', 'chat_completions')).toBe(
    'https://gateway.test/team/v1/chat/completions',
  );
  expect(providerRequestURL('', 'messages')).toBe('');
});
