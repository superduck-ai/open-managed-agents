import { describe, expect, test } from 'bun:test';
import { sessionResourceDisplayName, sessionResourceIdentity } from './session-resource-display';

describe('session resource table display', () => {
  test('uses snapshot name and memory store id for memory stores', () => {
    const resource = {
      type: 'memory_store',
      name: 'test',
      memory_store_id: 'memstore_YJfNRv',
      mount_path: '/mnt/memory/test',
    };

    expect(sessionResourceDisplayName(resource, { file_orders123456: 'source-orders.zip' })).toBe('test');
    expect(sessionResourceIdentity(resource)).toBe('memstore_YJfNRv');
  });

  test('does not fall back to file fields for memory stores', () => {
    const resource = {
      type: 'memory_store',
      file_id: 'file_orders123456',
      memory_store_id: '',
      name: '',
    };

    expect(sessionResourceDisplayName(resource, { file_orders123456: 'source-orders.zip' })).toBe('—');
    expect(sessionResourceIdentity(resource)).toBe('—');
  });

  test('keeps file name lookup and file id for file resources', () => {
    const resource = {
      type: 'file',
      file_id: 'file_orders123456',
      name: 'ignored snapshot name',
      memory_store_id: 'memstore_should_not_show',
    };

    expect(sessionResourceDisplayName(resource, { file_orders123456: 'source-orders.zip' })).toBe('source-orders.zip');
    expect(sessionResourceIdentity(resource)).toBe('file_orders123456');
  });

  test('shows github repository url as name and identity', () => {
    const resource = {
      type: 'github_repository',
      url: 'https://github.com/acme/app',
      file_id: 'file_orders123456',
    };

    expect(sessionResourceDisplayName(resource, { file_orders123456: 'source-orders.zip' })).toBe(
      'https://github.com/acme/app',
    );
    expect(sessionResourceIdentity(resource)).toBe('https://github.com/acme/app');
  });
});
