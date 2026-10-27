import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { createSessionAttachment, prepareSessionAttachments } from './sessionAttachments';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test.each(['\u0000', '\t', '\n', '\r', '\u001f', '\u007f', '\u0085', '\u009f'])(
  'makes a safe mount filename for control character %j',
  (control) => {
    const file = new File(['data'], `订单${control}结果.csv`);
    const attachment = createSessionAttachment(file);
    expect(attachment.file).toBe(file);
    expect(attachment.mountPath).toBe(`attachments/${attachment.id}/订单_结果.csv`);
  },
);

test.each(['', '.', '..', '   '])('uses a usable fallback for filename %j', (name) => {
  const file = new File(['data'], name || 'empty');
  if (!name) Object.defineProperty(file, 'name', { value: name });
  const attachment = createSessionAttachment(file);
  expect(attachment.mountPath).toBe(`attachments/${attachment.id}/file`);
});

test('normalizes upload names and mount paths while preserving bytes, file properties and retry progress', async () => {
  resetTestDom('https://oma.duck.ai/');
  const original = new File(['商品,数量\n苹果,2\n'], '订单\t\n\u007f\\/<>:"|?*.csv', {
    type: 'text/csv',
    lastModified: 123456,
  });
  const attachment = createSessionAttachment(original);
  let uploaded: File | undefined;
  const mounts: string[] = [];
  let uploads = 0;
  globalThis.fetch = async (input, init) => {
    if (new URL(String(input), 'https://oma.duck.ai').pathname === '/v1/files') {
      uploads++;
      uploaded = (init?.body as FormData).get('file') as File;
      return Response.json({ id: 'file_control', filename: uploaded.name, type: 'file' });
    }
    mounts.push(JSON.parse(String(init?.body)).mount_path);
    if (mounts.length === 1)
      return Response.json({ error: { type: 'api_error', message: 'Temporary mount failure' } }, { status: 500 });
    return Response.json({ id: 'resource_control', file_id: 'file_control', type: 'file' });
  };
  await expect(prepareSessionAttachments([attachment], 'sesn_control', 'workspace_control', () => {})).rejects.toThrow(
    'Temporary mount failure',
  );
  const paths = await prepareSessionAttachments([attachment], 'sesn_control', 'workspace_control', () => {});
  expect(uploads).toBe(1);
  expect(uploaded?.name).not.toMatch(/[\\/<>:"|?*\p{Cc}]/u);
  expect(uploaded?.type).toBe(original.type);
  expect(original.lastModified).toBe(123456);
  expect(original.name).toBe('订单\t\n\u007f\\/<>:"|?*.csv');
  expect(await uploaded?.text()).toBe(await original.text());
  expect(attachment.file).toBe(original);
  expect(mounts).toEqual([`/${attachment.mountPath}`, `/${attachment.mountPath}`]);
  expect(mounts[0]).not.toMatch(/\p{Cc}/u);
  expect(attachment.mountPath.endsWith(`/${uploaded?.name}`)).toBe(true);
  expect(paths).toEqual([`/mnt/session/uploads${mounts[0]}`]);
  expect(attachment.mounted).toBe(true);
});

test('repairs an unsafe unmounted path with cached upload metadata without uploading again', async () => {
  resetTestDom('https://oma.duck.ai/');
  const attachment = createSessionAttachment(new File(['data'], '订单\u007f.csv'));
  attachment.mountPath = `attachments/${attachment.id}/订单\u007f.csv`;
  attachment.metadata = {
    id: 'file_cached',
    filename: '订单\u007f.csv',
    type: 'file',
    mime_type: 'text/csv',
    size_bytes: 4,
    created_at: '',
  };
  const requests: string[] = [];
  globalThis.fetch = async (input, init) => {
    requests.push(new URL(String(input), 'https://oma.duck.ai').pathname);
    const body = JSON.parse(String(init?.body));
    expect(body.file_id).toBe('file_cached');
    expect(body.mount_path).toBe(`/attachments/${attachment.id}/订单_.csv`);
    return Response.json({ id: 'resource_cached', file_id: 'file_cached', type: 'file' });
  };
  await prepareSessionAttachments([attachment], 'sesn_cached', 'workspace_cached', () => {});
  expect(requests).toEqual(['/v1/sessions/sesn_cached/resources']);
  expect(attachment.mounted).toBe(true);
});
