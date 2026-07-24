#!/usr/bin/env bun

import assert from 'node:assert/strict';

import Anthropic, { toFile } from '@anthropic-ai/sdk';
import type { BetaManagedAgentsModel } from '@anthropic-ai/sdk/resources/beta';

type CliOptions = {
  apiKey: string;
  baseURL: string;
  environmentID?: string;
  keepResources: boolean;
  model: string;
  mountPath: string;
  title: string;
};

function readEnv(...names: string[]): string | undefined {
  for (const name of names) {
    const value = process.env[name]?.trim();
    if (value) {
      return value;
    }
  }
  return undefined;
}

function usage(): string {
  return [
    'Usage:',
    '  bun run run-agent-files',
    '',
    'Options:',
    '  --environment-id <id>        Existing environment ID. Defaults to ENVIRONMENT_ID/ANTHROPIC_ENVIRONMENT_ID',
    '  --base-url <url>             API base URL. Defaults to TEST_API_BASE_URL/ANTHROPIC_BASE_URL/http://127.0.0.1:18080',
    '  --api-key <key>              API key. Defaults to TEST_API_KEY/ANTHROPIC_API_KEY/sk-ant-local-default',
    '  --model <model>              Model used when creating the temporary agent. Defaults to claude-sonnet-4-6',
    '  --mount-path <path>          File mount_path to test. Defaults to /data.csv',
    '  --title <title>              Session title. Defaults to Files smoke session',
    '  --keep-resources             Keep the temporary uploaded files, agent, environment, and session',
    '  -h, --help                   Show this help',
  ].join('\n');
}

function readValue(argv: string[], index: number, flag: string): string {
  const value = argv[index + 1];
  if (!value || value.startsWith('-')) {
    throw new Error(`${flag} requires a value`);
  }
  return value;
}

function parseArgs(argv: string[]): CliOptions {
  let apiKey = readEnv('TEST_API_KEY', 'ANTHROPIC_API_KEY') ?? 'sk-ant-local-default';
  let baseURL = readEnv('TEST_API_BASE_URL', 'ANTHROPIC_BASE_URL') ?? 'http://127.0.0.1:18080';
  let environmentID = readEnv('ENVIRONMENT_ID', 'ANTHROPIC_ENVIRONMENT_ID');
  let keepResources = false;
  let model = readEnv('AGENT_MODEL', 'ANTHROPIC_AGENT_MODEL') ?? 'claude-sonnet-4-6';
  let mountPath = readEnv('SESSION_FILE_MOUNT_PATH') ?? '/data.csv';
  let title = readEnv('SESSION_TITLE') ?? 'Files smoke session';

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    switch (arg) {
      case '-h':
      case '--help':
        console.log(usage());
        process.exit(0);
      case '--api-key':
        apiKey = readValue(argv, i, arg);
        i += 1;
        break;
      case '--base-url':
        baseURL = readValue(argv, i, arg);
        i += 1;
        break;
      case '--environment-id':
      case '--env-id':
        environmentID = readValue(argv, i, arg);
        i += 1;
        break;
      case '--keep-resources':
        keepResources = true;
        break;
      case '--model':
        model = readValue(argv, i, arg);
        i += 1;
        break;
      case '--mount-path':
        mountPath = readValue(argv, i, arg);
        i += 1;
        break;
      case '--title':
        title = readValue(argv, i, arg);
        i += 1;
        break;
      default:
        throw new Error(`Unknown option: ${arg}`);
    }
  }

  if (!mountPath.startsWith('/')) {
    throw new Error(`mount_path must be absolute, got ${mountPath}`);
  }

  return {
    apiKey,
    baseURL,
    environmentID,
    keepResources,
    model,
    mountPath,
    title,
  };
}

function textFromBlocks(content: Array<{ text?: string }>): string {
  let combined = '';
  for (const block of content) {
    if (typeof block.text === 'string') {
      combined += block.text;
    }
  }
  return combined;
}

function cleanupErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message.split('\n')[0] ?? error.message;
  }
  return String(error);
}

async function sleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

async function deleteEnvironmentWithRetry(
  client: Anthropic,
  environmentID: string,
): Promise<void> {
  const deadline = Date.now() + 20_000;
  while (true) {
    try {
      await client.beta.environments.delete(environmentID);
      return;
    } catch (error) {
      const status = (error as { status?: number }).status;
      const message = error instanceof Error ? error.message : String(error);
      if (status === 400 && message.includes('Environment has active work') && Date.now() < deadline) {
        await sleep(500);
        continue;
      }
      throw error;
    }
  }
}

async function verifyMountedFile(
  client: Anthropic,
  sessionID: string,
  sandboxVisiblePath: string,
  expectedContent: string,
): Promise<void> {
  const stream = await client.beta.sessions.events.stream(sessionID);
  let agentText = '';

  try {
    await client.beta.sessions.events.send(sessionID, {
      events: [
        {
          type: 'user.message',
          content: [
            {
              type: 'text',
              text: [
                `Use bash to read ${sandboxVisiblePath}.`,
                'Reply with only the exact file contents.',
                'Do not add markdown, quotes, or any extra words.',
              ].join(' '),
            },
          ],
        },
      ],
    });

    for await (const event of stream) {
      if (event.type === 'agent.message') {
        const chunk = textFromBlocks(event.content);
        agentText += chunk;
        process.stdout.write(chunk);
      } else if (event.type === 'agent.tool_use') {
        console.log(`\n[Using tool: ${event.name}]`);
      } else if (event.type === 'agent.custom_tool_use') {
        console.log(`\n[Using custom tool: ${event.name}]`);
      } else if (event.type === 'session.error') {
        throw new Error(`session error: ${JSON.stringify(event.error)}`);
      } else if (event.type === 'session.status_idle') {
        break;
      } else if (event.type === 'session.status_terminated') {
        throw new Error('session terminated before the file verification finished');
      }
    }
  } finally {
    stream.controller.abort();
  }

  console.log();
  assert.equal(
    agentText.trim(),
    expectedContent.trim(),
    `agent should echo the file mounted at ${sandboxVisiblePath}`,
  );
}

async function main(): Promise<void> {
  const options = parseArgs(process.argv.slice(2));
  const client = new Anthropic({
    apiKey: options.apiKey,
    baseURL: options.baseURL,
    maxRetries: 0,
  });

  const primaryFilename = 'managed-agents-files-smoke.csv';
  const primaryContent = 'name,value\nalpha,1';
  const secondaryFilename = 'managed-agents-files-added.json';
  const secondaryContent = '{"ok":true}';
  const visibleMountPath = `/mnt/session/uploads${options.mountPath}`;

  let createdAgentID: string | undefined;
  let createdEnvironmentID: string | undefined;
  let createdSessionID: string | undefined;
  const uploadedFileIDs: string[] = [];

  try {
    const agent = await client.beta.agents.create({
      name: `js-test-files-agent-${Date.now()}`,
      model: options.model as BetaManagedAgentsModel,
      system: [
        'You are a verification agent.',
        'When asked to read a file, use bash and reply with only the exact file contents.',
        'Do not add markdown, labels, or explanations.',
      ].join(' '),
      tools: [
        {
          type: 'agent_toolset_20260401',
        },
      ],
    });
    createdAgentID = agent.id;
    console.log(`Agent ID: ${agent.id}, version: ${agent.version}`);

    let environmentID = options.environmentID;
    if (!environmentID) {
      const environment = await client.beta.environments.create({
        name: `js-test-files-env-${Date.now()}`,
        config: {
          type: 'cloud',
          networking: { type: 'unrestricted' },
        },
      });
      environmentID = environment.id;
      createdEnvironmentID = environment.id;
      console.log(`Environment ID: ${environment.id}`);
    }

    const primaryUpload = await client.beta.files.upload({
      file: await toFile(Buffer.from(primaryContent), primaryFilename),
    });
    uploadedFileIDs.push(primaryUpload.id);
    console.log(`Uploaded file: ${primaryUpload.id} (${primaryUpload.filename})`);

    const session = await client.beta.sessions.create({
      agent: agent.id,
      environment_id: environmentID,
      title: options.title,
      resources: [
        {
          type: 'file',
          file_id: primaryUpload.id,
          mount_path: options.mountPath,
        },
      ],
    });
    createdSessionID = session.id;
    console.log(`Session ID: ${session.id}`);

    const createdFileResource = session.resources.find((resource) => resource.type === 'file');
    assert.ok(createdFileResource, 'session should expose the created file resource');
    assert.equal(createdFileResource.file_id, primaryUpload.id);
    assert.equal(createdFileResource.mount_path, options.mountPath);

    await verifyMountedFile(client, session.id, visibleMountPath, primaryContent);

    const addedUpload = await client.beta.files.upload({
      file: await toFile(Buffer.from(secondaryContent), secondaryFilename),
    });
    uploadedFileIDs.push(addedUpload.id);
    console.log(`Uploaded file for resources.add: ${addedUpload.id} (${addedUpload.filename})`);

    const addedResource = await client.beta.sessions.resources.add(session.id, {
      type: 'file',
      file_id: addedUpload.id,
    });
    assert.equal(addedResource.file_id, addedUpload.id);
    assert.ok(
      addedResource.mount_path.endsWith(addedUpload.id),
      `expected default mount_path to end with ${addedUpload.id}, got ${addedResource.mount_path}`,
    );

    const resourcesPage = await client.beta.sessions.resources.list(session.id);
    assert.ok(
      resourcesPage.data.some(
        (resource) => resource.type === 'file' && resource.id === addedResource.id,
      ),
      'resources.list should include the file added after session creation',
    );

    const deletedResource = await client.beta.sessions.resources.delete(addedResource.id, {
      session_id: session.id,
    });
    assert.equal(deletedResource.id, addedResource.id);
    assert.equal(deletedResource.type, 'session_resource_deleted');

    const resourcesAfterDelete = await client.beta.sessions.resources.list(session.id);
    assert.ok(
      !resourcesAfterDelete.data.some(
        (resource) => resource.type === 'file' && resource.id === addedResource.id,
      ),
      'resources.delete should remove the added file resource',
    );

    console.log('Managed Agents files smoke test passed.');
  } finally {
    if (!options.keepResources) {
      if (createdSessionID) {
        await client.beta.sessions.delete(createdSessionID).catch((error: unknown) => {
          console.error(
            `Failed to delete temporary session ${createdSessionID}: ${cleanupErrorMessage(error)}`,
          );
        });
      }
      for (const fileID of uploadedFileIDs) {
        await client.beta.files.delete(fileID).catch((error: unknown) => {
          console.error(`Failed to delete uploaded file ${fileID}: ${cleanupErrorMessage(error)}`);
        });
      }
      if (createdEnvironmentID) {
        await deleteEnvironmentWithRetry(client, createdEnvironmentID).catch((error: unknown) => {
          const message = cleanupErrorMessage(error);
          if (message.includes('Environment has active work')) {
            console.error(
              `Temporary environment ${createdEnvironmentID} still has active work and was left in place.`,
            );
            return;
          }
          console.error(`Failed to delete temporary environment ${createdEnvironmentID}: ${message}`);
        });
      }
      if (createdAgentID) {
        await client.beta.agents.archive(createdAgentID).catch((error: unknown) => {
          console.error(
            `Failed to archive temporary agent ${createdAgentID}: ${cleanupErrorMessage(error)}`,
          );
        });
      }
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
