import { afterEach, expect, test } from 'bun:test';
import { cleanup, render } from '@testing-library/react';
import { OAuthProviderAvatar } from './oauth-provider-avatar';

afterEach(cleanup);

test.each(['feishu', 'friday'])('内置 %s 使用默认品牌头像', (provider) => {
  const { container } = render(<OAuthProviderAvatar providerKey={`builtin:${provider}`} />);
  const image = container.querySelector('img');
  expect(image?.getAttribute('src')).toContain(provider);
  expect(image?.classList.contains('dark:invert')).toBe(provider === 'friday');
});

test('自定义 Logo 优先且不应用默认品牌反色', () => {
  const { container } = render(
    <OAuthProviderAvatar providerKey="builtin:friday" logoUrl="https://example.com/custom.png" />,
  );
  const image = container.querySelector('img');
  expect(image?.getAttribute('src')).toBe('https://example.com/custom.png');
  expect(image?.classList.contains('dark:invert')).toBe(false);
});

test('无 Logo 的自定义服务方保留通用图标', () => {
  const { container } = render(<OAuthProviderAvatar providerKey="custom:example" />);
  expect(container.querySelector('img')).toBeNull();
  expect(container.querySelector('svg')).toBeTruthy();
});
