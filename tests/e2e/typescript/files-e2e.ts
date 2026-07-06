import assert from 'node:assert/strict';
import Anthropic, { toFile } from '@anthropic-ai/sdk';

const baseURL = process.env['TEST_API_BASE_URL'] ?? 'http://127.0.0.1:18080';
const apiKey = process.env['TEST_API_KEY'] ?? 'sk-ant-local-default';

const client = new Anthropic({
  apiKey,
  baseURL,
  maxRetries: 0,
});

async function expectStatus(label: string, promise: Promise<unknown>, status: number): Promise<void> {
  try {
    await promise;
  } catch (error) {
    const actual = (error as { status?: number }).status;
    assert.equal(actual, status, `${label}: unexpected status`);
    return;
  }
  throw new Error(`${label}: expected request to fail with ${status}`);
}

async function main(): Promise<void> {
  await expectStatus(
    'missing file metadata',
    client.beta.files.retrieveMetadata('file_missing_ts_sdk_e2e'),
    404,
  );

  const nonDownloadable = await client.beta.files.upload({
    file: await toFile(Buffer.from('typescript sdk no download'), 'ts-sdk-no-download.txt'),
  });
  try {
    await expectStatus(
      'uploaded file download',
      client.beta.files.download(nonDownloadable.id),
      400,
    );
  } finally {
    await client.beta.files.delete(nonDownloadable.id).catch(() => undefined);
  }

  const uploaded = await client.beta.files.upload({
    file: await toFile(Buffer.from('hello from typescript sdk'), 'ts-sdk-e2e.txt'),
  });
  assert.equal(uploaded.type, 'file');
  assert.equal(uploaded.filename, 'ts-sdk-e2e.txt');

  const page = await client.beta.files.list({ limit: 20 });
  assert.ok(page.data.some((file) => file.id === uploaded.id), 'uploaded file not found in list');

  const retrieved = await client.beta.files.retrieveMetadata(uploaded.id);
  assert.equal(retrieved.id, uploaded.id);

  const deleted = await client.beta.files.delete(uploaded.id);
  assert.equal(deleted.id, uploaded.id);
  assert.equal(deleted.type, 'file_deleted');
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
