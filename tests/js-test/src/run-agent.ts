#!/usr/bin/env bun

import Anthropic from '@anthropic-ai/sdk';
import type { BetaManagedAgentsModel } from '@anthropic-ai/sdk/resources/beta';

type CliOptions = {
  agentID?: string;
  apiKey: string;
  baseURL: string;
  environmentID?: string;
  keepResources: boolean;
  message: string;
  model: string;
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
    '  bun run run-agent --message "write hello.txt"',
    '  bun run run-agent "write hello.txt"',
    '',
    'Options:',
    '  -m, --message <text>          User message to send to the agent',
    '  --agent-id <id>              Existing agent ID. Defaults to AGENT_ID/ANTHROPIC_AGENT_ID',
    '  --environment-id <id>        Existing environment ID. Defaults to ENVIRONMENT_ID/ANTHROPIC_ENVIRONMENT_ID',
    '  --base-url <url>             API base URL. Defaults to TEST_API_BASE_URL/ANTHROPIC_BASE_URL/http://127.0.0.1:18080',
    '  --api-key <key>              API key. Defaults to TEST_API_KEY/ANTHROPIC_API_KEY/sk-ant-local-default',
    '  --model <model>              Model used when creating a temporary agent. Defaults to claude-sonnet-4-6',
    '  --title <title>              Session title. Defaults to Quickstart session',
    '  --keep-resources             Keep any temporary agent/environment created by this command',
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
  const positional: string[] = [];
  let agentID = readEnv('AGENT_ID', 'ANTHROPIC_AGENT_ID');
  let apiKey = readEnv('TEST_API_KEY', 'ANTHROPIC_API_KEY') ?? 'sk-ant-local-default';
  let baseURL = readEnv('TEST_API_BASE_URL', 'ANTHROPIC_BASE_URL') ?? 'http://127.0.0.1:18080';
  let environmentID = readEnv('ENVIRONMENT_ID', 'ANTHROPIC_ENVIRONMENT_ID');
  let keepResources = false;
  let message = '';
  let model = readEnv('AGENT_MODEL', 'ANTHROPIC_AGENT_MODEL') ?? 'claude-sonnet-4-6';
  let title = readEnv('SESSION_TITLE') ?? 'Quickstart session';

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    switch (arg) {
      case '-h':
      case '--help':
        console.log(usage());
        process.exit(0);
      case '-m':
      case '--message':
        message = readValue(argv, i, arg);
        i += 1;
        break;
      case '--agent-id':
        agentID = readValue(argv, i, arg);
        i += 1;
        break;
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
      case '--title':
        title = readValue(argv, i, arg);
        i += 1;
        break;
      case '--':
        positional.push(...argv.slice(i + 1));
        i = argv.length;
        break;
      default:
        if (arg.startsWith('-')) {
          throw new Error(`Unknown option: ${arg}`);
        }
        positional.push(arg);
    }
  }

  if (!message) {
    message = positional.join(' ').trim();
  }
  if (!message) {
    throw new Error(`Missing user message.\n\n${usage()}`);
  }

  return {
    agentID,
    apiKey,
    baseURL,
    environmentID,
    keepResources,
    message,
    model,
    title,
  };
}

function writeAgentText(content: Array<{ text?: string }>): void {
  for (const block of content) {
    if (typeof block.text === 'string') {
      process.stdout.write(block.text);
    }
  }
}

async function main(): Promise<void> {
  const options = parseArgs(process.argv.slice(2));
  const client = new Anthropic({
    apiKey: options.apiKey,
    baseURL: options.baseURL,
    maxRetries: 0,
  });

  let createdAgentID: string | undefined;
  let createdEnvironmentID: string | undefined;
  let agentID = options.agentID;
  let environmentID = options.environmentID;

  try {
    if (!agentID) {
      const agent = await client.beta.agents.create({
        name: `js-test-agent-${Date.now()}`,
        model: options.model as BetaManagedAgentsModel,
        system: 'You are a helpful coding assistant. Write clean, well-documented code.',
        tools: [
          {
            type: 'agent_toolset_20260401',
          },
        ],
      });
      agentID = agent.id;
      createdAgentID = agent.id;
      console.log(`Agent ID: ${agent.id}, version: ${agent.version}`);
    }

    if (!environmentID) {
      const environment = await client.beta.environments.create({
        name: `js-test-env-${Date.now()}`,
        config: {
          type: 'cloud',
          networking: { type: 'unrestricted' },
        },
      });
      environmentID = environment.id;
      createdEnvironmentID = environment.id;
      console.log(`Environment ID: ${environment.id}`);
    }

    const session = await client.beta.sessions.create({
      agent: agentID,
      environment_id: environmentID,
      title: options.title,
    });

    console.log(`Session ID: ${session.id}`);

    const stream = await client.beta.sessions.events.stream(session.id);

    await client.beta.sessions.events.send(session.id, {
      events: [
        {
          type: 'user.message',
          content: [
            {
              type: 'text',
              text: options.message,
            },
          ],
        },
      ],
    });

    for await (const event of stream) {
      if (event.type === 'agent.message') {
        writeAgentText(event.content);
      } else if (event.type === 'agent.tool_use') {
        console.log(`\n[Using tool: ${event.name}]`);
      } else if (event.type === 'agent.custom_tool_use') {
        console.log(`\n[Using custom tool: ${event.name}]`);
      } else if (event.type === 'session.error') {
        console.error('\nSession error:', JSON.stringify(event.error, null, 2));
      } else if (event.type === 'session.status_idle') {
        console.log('\n\nAgent finished.');
        break;
      } else if (event.type === 'session.status_terminated') {
        console.log('\n\nSession terminated.');
        break;
      }
    }
  } finally {
    if (!options.keepResources) {
      if (createdEnvironmentID) {
        await client.beta.environments.delete(createdEnvironmentID).catch((error: unknown) => {
          console.error(`Failed to delete temporary environment ${createdEnvironmentID}:`, error);
        });
      }
      if (createdAgentID) {
        await client.beta.agents.archive(createdAgentID).catch((error: unknown) => {
          console.error(`Failed to archive temporary agent ${createdAgentID}:`, error);
        });
      }
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
