import { afterEach, expect, test } from 'bun:test';
import { cleanup, render } from '@testing-library/react';
import { OAuthProviderAvatar } from './oauth-provider-avatar';

afterEach(cleanup);

test('头像直接使用配置中的 URL', () => {
  const { container } = render(<OAuthProviderAvatar logoUrl="https://example.com/logo.png" />);
  expect(container.querySelector('img')?.getAttribute('src')).toBe('https://example.com/logo.png');
});

test('未配置 Logo 时显示通用图标', () => {
  const { container } = render(<OAuthProviderAvatar />);
  expect(container.querySelector('img')).toBeNull();
  expect(container.querySelector('svg')).toBeTruthy();
});
