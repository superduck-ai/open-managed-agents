import { describe, expect, test } from 'bun:test';

import {
  DEFAULT_TUNNEL_CHANNEL,
  MCP_TUNNEL_CHANNEL_PATTERN,
  tunnelChannelFromURL,
  tunnelChannelServerName,
  tunnelChannelURL,
  tunnelIdFromPath,
} from './config';

const tunnel = {
  mcp_url: 'https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef',
};

describe('MCP tunnel presentation helpers', () => {
  test('resolves main and named channel canonical URLs', () => {
    expect(tunnelChannelURL(tunnel, DEFAULT_TUNNEL_CHANNEL)).toBe(tunnel.mcp_url);
    expect(tunnelChannelURL(tunnel, 'secondary')).toBe(`${tunnel.mcp_url}/secondary`);
  });

  test('uses a stable channel suffix for every channel', () => {
    const tunnelID = 'tunnel_0123456789abcdef0123456789abcdef';
    expect(tunnelChannelServerName(tunnelID, 'main')).toBe(`${tunnelID}__main`);
    expect(tunnelChannelServerName(tunnelID, 'secondary')).toBe(`${tunnelID}__secondary`);
  });

  test('round-trips configured channel URLs and rejects unrelated targets', () => {
    expect(tunnelChannelFromURL(tunnel, tunnel.mcp_url)).toBe('main');
    expect(tunnelChannelFromURL(tunnel, `${tunnel.mcp_url}/secondary`)).toBe('secondary');
    expect(tunnelChannelFromURL(tunnel, 'https://other.example/mcp')).toBeNull();
    expect(tunnelChannelFromURL(tunnel, `${tunnel.mcp_url}/bad/path`)).toBeNull();
  });

  test('shares the connector channel contract and parses detail route IDs', () => {
    expect(MCP_TUNNEL_CHANNEL_PATTERN.test('lowercase_1-ok')).toBe(true);
    expect(MCP_TUNNEL_CHANNEL_PATTERN.test('Uppercase')).toBe(false);
    expect(tunnelIdFromPath('/settings/workspaces/default/mcp-tunnels/tunnel_test')).toBe('tunnel_test');
  });
});
