import { expect, test } from 'bun:test';
import { providerBaseURL, providerRequestURL, providerForm, providerFormInput } from './provider-form';
import type { LLMProvider } from './api';

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

test('editing a provider retains image budgets on its model', () => {
  const provider: LLMProvider = {
    api_backend: 'messages',
    type: 'llm_provider',
    id: 'provider',
    name: 'Vision',
    slug: 'vision',
    base_url: 'https://models.example.com',
    has_api_key: true,
    api_key_last4: 'test',
    created_at: '',
    updated_at: '',
    models: [
      { id: 'vision-model', input_modalities: ['text', 'image'], image_input: { max_images: 3, max_pixels: 1048576 } },
    ],
  };
  const form = providerForm(provider);
  form.name = 'Renamed';
  const result = providerFormInput(form, provider, false, (_key, fallback) => fallback);
  expect('input' in result && result.input.models[0]?.image_input).toEqual({ max_images: 3, max_pixels: 1048576 });
});
