import { afterEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../../../test/setup';

const { cleanup, fireEvent, render } = await import('@testing-library/react');
const { RemoteServerIcon } = await import('./RemoteServerIcon');

afterEach(() => cleanup());

describe('RemoteServerIcon', () => {
  test('preserves favicon service URLs whose image type is expressed by the endpoint', () => {
    resetTestDom('https://oma.duck.ai/');
    const iconUrl = 'https://t0.gstatic.com/faviconV2?client=SOCIAL&type=FAVICON&url=https://mail.google.com&size=64';
    const rendered = render(<RemoteServerIcon iconUrl={iconUrl} serverUrl="https://gmailmcp.googleapis.com/mcp/v1" />);

    expect(rendered.container.querySelector('img')?.getAttribute('src')).toBe(iconUrl);
  });

  test('uses the origin favicon when Directory returns a webpage instead of an image', () => {
    resetTestDom('https://oma.duck.ai/');
    const rendered = render(<RemoteServerIcon iconUrl="https://www.canva.com/" />);

    expect(rendered.container.querySelector('img')?.getAttribute('src')).toBe('https://www.canva.com/favicon.ico');
  });

  test('tries the MCP server favicon after a direct Directory image fails', () => {
    resetTestDom('https://oma.duck.ai/');
    const rendered = render(
      <RemoteServerIcon
        iconUrl="https://assets.example.com/icons/product.png"
        serverUrl="https://mcp.example.com/v1/mcp"
      />,
    );
    const directoryImage = rendered.container.querySelector('img') as HTMLImageElement;

    expect(directoryImage.getAttribute('src')).toBe('https://assets.example.com/icons/product.png');
    fireEvent.error(directoryImage);
    const serverFavicon = rendered.container.querySelector('img') as HTMLImageElement;
    expect(serverFavicon.getAttribute('src')).toBe('https://mcp.example.com/favicon.ico');
    fireEvent.error(serverFavicon);
    const publicFavicon = rendered.container.querySelector('img') as HTMLImageElement;
    expect(publicFavicon.getAttribute('src')).toBe('https://www.google.com/s2/favicons?domain=mcp.example.com&sz=64');
    fireEvent.error(publicFavicon);
    expect(rendered.container.querySelector('img')).toBeNull();
    expect(rendered.container.querySelector('.lucide-server')).toBeTruthy();
  });
});
