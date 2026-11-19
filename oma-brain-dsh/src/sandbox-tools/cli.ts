import { createHash } from "node:crypto";
import { ToolNotForwardedError, type RemoteTool } from "../runtime/contracts.ts";
import type { Payload } from "../json.ts";
import type { OAuthRequirement } from "../runtime/oauth.ts";

export interface CliDefinition {
  name: string;
  description: string;
  executable: string;
  oauth_variable: string;
  target_host: string;
  subcommands: string[];
  blocked_flags?: string[];
  public_help?: boolean;
}

export function cliTool(cli: CliDefinition): RemoteTool {
  return {
    name: cli.name, remoteName: cli.name, permissionName: "bash", description: cli.description,
    inputSchema: { type: "object", required: ["args"], additionalProperties: false,
      properties: { args: { type: "array", items: { type: "string" } } } },
  };
}

export interface CliInvocation {
  credential?: Pick<OAuthRequirement, "secret_name" | "target_host" | "invocation_id">;
  command(placeholder: string): Payload;
}

export function cliInvocation(cli: CliDefinition, args: Payload, threadId?: string): CliInvocation {
  const argv: unknown = args.args;
  if ((threadId && threadId !== "root") || Object.keys(args).length !== 1 ||
      !Array.isArray(argv) || !cli.subcommands.includes(argv[0]) ||
      argv.some(arg => typeof arg !== "string" || arg.includes("\0") || cli.blocked_flags?.includes(arg.split("=", 1)[0])))
    throw new ToolNotForwardedError(`${cli.name} 参数无效`);
  const help = cli.public_help && ["--help", "-h"].includes(argv.at(-1)) &&
    argv.slice(0,-1).every(arg => /^[a-z][a-z-]*$/.test(arg));
  return {
    credential: help ? undefined : {
      secret_name: cli.oauth_variable, target_host: cli.target_host,
      invocation_id: createHash("sha256").update(JSON.stringify(argv)).digest("hex"),
    },
    command(placeholder) {
      const command = [cli.executable,...argv].map(arg => "'" + arg.replaceAll("'", "'\\''") + "'").join(" ");
      return {command, env: {[cli.oauth_variable]:placeholder}};
    },
  };
}
