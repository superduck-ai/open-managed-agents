# JS Agent Smoke Command

Run a Managed Agents session with the Anthropic TypeScript SDK:

```bash
cd tests/js-test
bun install
bun run run-agent --message "Create a Python script that writes the first 20 Fibonacci numbers to fibonacci.txt"
```

By default the command targets the local API server:

- `TEST_API_BASE_URL` or `ANTHROPIC_BASE_URL`, default `http://127.0.0.1:18080`
- `TEST_API_KEY` or `ANTHROPIC_API_KEY`, default `sk-ant-local-default`

To reuse an existing agent and environment:

```bash
bun run run-agent \
  --agent-id agent_... \
  --environment-id env_... \
  --message "List the files in the workspace"
```

If `--agent-id` or `--environment-id` is omitted, the command creates temporary quickstart resources and cleans them up after the stream reaches idle. Add `--keep-resources` to keep those resources for inspection.

To exercise the Managed Agents files flow from the Claude docs, run the dedicated smoke case:

```bash
cd tests/js-test
bun install
bun run run-agent-files --base-url http://127.0.0.1:38080
```

This smoke case verifies:

- `files.upload`
- `sessions.create(...resources=[{type:"file"}])` with an explicit `mount_path`
- agent reads the mounted file from `/mnt/session/uploads...`
- `sessions.resources.add/list/delete` with the default mount path behavior
