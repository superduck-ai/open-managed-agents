import { createHash } from "node:crypto";
import {
  ToolNotForwardedError,
  type RemoteTool,
} from "../runtime/contracts.ts";
import type { Payload } from "../json.ts";
import type { OAuthRequirement } from "../runtime/oauth.ts";

const fridayTool: RemoteTool = {
    name: "Friday", remoteName: "friday", permissionName: "bash",
    description: "查询 Friday 数据平台，使用当前用户的授权。传入 CLI 查询参数，例如 app list。需要授权时会在原对话中提示。",
    inputSchema: { type: "object", required: ["args"], additionalProperties: false,
      properties: { args: { type: "array", items: { type: "string" } } } },
  };

export const cliTools = [fridayTool];

export interface CliInvocation {
  credential?: Pick<
    OAuthRequirement,
    "secret_name" | "target_host" | "invocation_id"
  >;
  command(placeholder: string): Payload;
}

export function cliInvocation(
  tool: RemoteTool,
  args: Payload,
  threadId?: string,
): CliInvocation | undefined {
  if (tool.remoteName !== "friday") return;
  const argv = args.args;
  if (
    (threadId && threadId !== "root") ||
    Object.keys(args).length !== 1 ||
    !Array.isArray(argv) ||
    ![
      "app",
      "mart",
      "event",
      "schema",
      "meta",
      "chart",
      "property",
      "dashboard",
      "analysis",
      "sql",
      "log",
    ].includes(argv[0]) ||
    argv.some(
      (arg) =>
        typeof arg !== "string" ||
        arg.includes("\0") ||
        /^--(api-key|base-url|proxy-auth|skip-ssl-verify)(=|$)/.test(arg),
    )
  )
    throw new ToolNotForwardedError("Friday 查询参数无效");
  const help =
    ["--help", "-h"].includes(argv.at(-1)) &&
    argv.slice(0, -1).every((arg) => /^[a-z][a-z-]*$/.test(arg));

  return {
    credential: help
      ? undefined
      : {
          secret_name: "FRIDAY_4A_TOKEN",
          target_host: "friday-tx.seewo.com",
          invocation_id: createHash("sha256")
            .update(JSON.stringify(argv))
            .digest("hex"),
        },
    command(placeholder) {
      const quoted = argv
        .map((arg) => "'" + arg.replaceAll("'", "'\\''") + "'")
        .join(" ");
      return {
        command:
          'set -e; FRIDAY_CLI_CONFIG_DIR="$(mktemp -d)"; export FRIDAY_CLI_CONFIG_DIR; trap \'rm -rf "$FRIDAY_CLI_CONFIG_DIR"\' EXIT; unset FRIDAY_API_KEY OMA_FRIDAY_OAUTH_PROVIDER OMA_TOOL_CALL_ID; "${OMA_FRIDAY_EXECUTABLE:-friday}" ' +
          quoted,
        env: {
          FRIDAY_BASE_URL: "https://friday-tx.seewo.com",
          FRIDAY_PROXY_AUTH: "0",
          FRIDAY_SKIP_SSL_VERIFY: "false",
          FRIDAY_4A_TOKEN: placeholder,
        },
      };
    },
  };
}
