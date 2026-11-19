import type { CliDefinition } from "../src/sandbox-tools/cli.ts";

export const lookupCli: CliDefinition = {
  name: "Lookup", description: "查询测试目录", executable: "lookup", oauth_variable: "LOOKUP_TOKEN",
  target_host: "lookup.example.org", subcommands: ["app"], blocked_flags: ["--base-url"], public_help: true,
};
