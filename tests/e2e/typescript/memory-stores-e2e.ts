import assert from 'node:assert/strict';
import Anthropic from '@anthropic-ai/sdk';

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
  await expectStatus('missing memory store', client.beta.memoryStores.retrieve('memstore_missing_ts_sdk_e2e'), 404);

  const store = await client.beta.memoryStores.create({
    name: 'ts-sdk-memory-store',
    description: 'typescript sdk memory e2e',
    metadata: { sdk: 'typescript' },
  });
  assert.equal(store.type, 'memory_store');
  assert.equal(store.metadata.sdk, 'typescript');

  try {
    const listedStores = await client.beta.memoryStores.list({ limit: 20 });
    assert.ok(listedStores.data.some((item) => item.id === store.id), 'store not found in list');

    const retrievedStore = await client.beta.memoryStores.retrieve(store.id);
    assert.equal(retrievedStore.id, store.id);

    const updatedStore = await client.beta.memoryStores.update(store.id, {
      name: 'ts-sdk-memory-store-updated',
      description: '',
      metadata: { phase: 'updated' },
    });
    assert.equal(updatedStore.name, 'ts-sdk-memory-store-updated');
    assert.equal(updatedStore.metadata.phase, 'updated');

    const memory = await client.beta.memoryStores.memories.create(store.id, {
      path: '/ts-sdk/notes.md',
      content: 'hello from typescript sdk memory',
      view: 'full',
    });
    assert.equal(memory.type, 'memory');
    assert.equal(memory.memory_store_id, store.id);
    assert.equal(memory.content, 'hello from typescript sdk memory');
    const firstVersionID = memory.memory_version_id;

    await expectStatus(
      'duplicate memory path',
      client.beta.memoryStores.memories.create(store.id, {
        path: '/ts-sdk/notes.md',
        content: 'duplicate',
      }),
      409,
    );

    const updatedMemory = await client.beta.memoryStores.memories.update(memory.id, {
      memory_store_id: store.id,
      path: '/ts-sdk/renamed.md',
      content: 'updated from typescript sdk memory',
      view: 'full',
      precondition: { type: 'content_sha256', content_sha256: memory.content_sha256 },
    });
    assert.equal(updatedMemory.id, memory.id);
    assert.equal(updatedMemory.path, '/ts-sdk/renamed.md');
    assert.equal(updatedMemory.content, 'updated from typescript sdk memory');
    assert.notEqual(updatedMemory.memory_version_id, firstVersionID);

    const retrievedMemory = await client.beta.memoryStores.memories.retrieve(memory.id, {
      memory_store_id: store.id,
    });
    assert.equal(retrievedMemory.content, updatedMemory.content);

    const memories = await client.beta.memoryStores.memories.list(store.id, {
      path_prefix: '/ts-sdk/',
      limit: 20,
    });
    assert.ok(memories.data.some((item) => item.type === 'memory' && item.id === memory.id), 'memory not found in list');

    const versions = await client.beta.memoryStores.memoryVersions.list(store.id, {
      memory_id: memory.id,
      limit: 20,
    });
    assert.ok(versions.data.some((item) => item.operation === 'created'), 'created version missing');
    assert.ok(versions.data.some((item) => item.operation === 'modified'), 'modified version missing');

    const firstVersion = await client.beta.memoryStores.memoryVersions.retrieve(firstVersionID, {
      memory_store_id: store.id,
      view: 'full',
    });
    assert.equal(firstVersion.content, memory.content);

    const redacted = await client.beta.memoryStores.memoryVersions.redact(firstVersionID, {
      memory_store_id: store.id,
    });
    assert.ok(redacted.redacted_at, 'redacted_at missing');
    assert.equal(redacted.content_sha256, null);
    assert.equal(redacted.path, null);

    const deletedMemory = await client.beta.memoryStores.memories.delete(memory.id, {
      memory_store_id: store.id,
      expected_content_sha256: updatedMemory.content_sha256,
    });
    assert.equal(deletedMemory.id, memory.id);
    assert.equal(deletedMemory.type, 'memory_deleted');
  } finally {
    const deletedStore = await client.beta.memoryStores.delete(store.id).catch(() => undefined);
    if (deletedStore) {
      assert.equal(deletedStore.id, store.id);
      assert.equal(deletedStore.type, 'memory_store_deleted');
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
