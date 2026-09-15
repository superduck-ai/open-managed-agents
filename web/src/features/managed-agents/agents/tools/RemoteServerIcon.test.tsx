import { afterEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../../../test/setup';

const { cleanup, fireEvent, render } = await import('@testing-library/react');
const { RemoteServerIcon } = await import('./RemoteServerIcon');

afterEach(() => cleanup());

describe('RemoteServerIcon', () => {
  test('preserves favicon service URLs whose image type is expressed by the endpoint', () => {
    resetTestDom('https://oma.duck.ai/');
    const iconUrl = 'https://t0.gstatic.com/faviconV2?client=SOCIAL&type=FAVICON&url=https://mail.google.com&size=64';
    const rendered = render(<RemoteServerIcon directoryIconUrl={iconUrl} />);

    expect(rendered.container.querySelector('img')?.getAttribute('src')).toBe(iconUrl);
  });

  test('uses the Directory origin favicon when icon_url is a webpage', () => {
    resetTestDom('https://oma.duck.ai/');
    const rendered = render(<RemoteServerIcon directoryIconUrl="https://www.canva.com/" />);

    const directoryFavicon = rendered.container.querySelector('img') as HTMLImageElement;
    expect(directoryFavicon.getAttribute('src')).toBe('https://www.canva.com/favicon.ico');
    fireEvent.error(directoryFavicon);
    expect(rendered.container.querySelector('img')?.getAttribute('src')).toBe(
      'https://www.google.com/s2/favicons?domain=www.canva.com&sz=64',
    );
  });

  test('uses only Directory-derived fallbacks after a direct image fails', () => {
    resetTestDom('https://oma.duck.ai/');
    const rendered = render(<RemoteServerIcon directoryIconUrl="https://assets.example.com/icons/product.png" />);
    const directoryImage = rendered.container.querySelector('img') as HTMLImageElement;

    expect(directoryImage.getAttribute('src')).toBe('https://assets.example.com/icons/product.png');
    fireEvent.error(directoryImage);
    const directoryFavicon = rendered.container.querySelector('img') as HTMLImageElement;
    expect(directoryFavicon.getAttribute('src')).toBe('https://assets.example.com/favicon.ico');
    fireEvent.error(directoryFavicon);
    const publicFavicon = rendered.container.querySelector('img') as HTMLImageElement;
    expect(publicFavicon.getAttribute('src')).toBe(
      'https://www.google.com/s2/favicons?domain=assets.example.com&sz=64',
    );
    fireEvent.error(publicFavicon);
    expect(rendered.container.querySelector('img')).toBeNull();
    expect(rendered.container.querySelector('.lucide-server')).toBeTruthy();
  });

  test('does not request an icon without a Directory icon_url', () => {
    resetTestDom('https://oma.duck.ai/');
    const rendered = render(<RemoteServerIcon />);

    expect(rendered.container.querySelector('img')).toBeNull();
    expect(rendered.container.querySelector('.lucide-server')).toBeTruthy();
  });
});
