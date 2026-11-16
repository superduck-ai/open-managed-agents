import { afterEach, expect, test } from 'bun:test';
import { cleanup, fireEvent, render } from '@testing-library/react';
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

test('图片失败后显示通用图标，更新 URL 后可重新加载', () => {
  const { container, rerender } = render(<OAuthProviderAvatar logoUrl="https://example.com/broken.png" />);
  fireEvent.error(container.querySelector('img')!);
  expect(container.querySelector('img')).toBeNull();
  expect(container.querySelector('svg')).toBeTruthy();
  rerender(<OAuthProviderAvatar logoUrl="https://example.com/new.png" />);
  expect(container.querySelector('img')?.getAttribute('src')).toBe('https://example.com/new.png');
  expect(container.querySelector('svg')).toBeNull();
});
