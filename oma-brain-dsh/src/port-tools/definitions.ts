import type { RemoteTool } from "../runtime/contracts.ts";

export function portToolDefinition(): RemoteTool {
  return {
    name: "get_port_url", remoteName: "port-url", permissionName: "get_port_url", readOnly: true,
    description: "Get the session port URL for an HTTP port inside this session's sandbox. The URL is fixed for this session and port: it is the same whether or not a service is already listening, so you can share it before the service starts. For the URL to work, a service must listen on that port inside the sandbox. A service listening only on 127.0.0.1 (localhost) is reachable through the URL as is, so do not ask the user to restart it with --host or --bind 0.0.0.0. Anyone who has the URL can open it, so share it only with the user. Never invent or guess a port URL or set up another tunnel; if this tool returns an error, report the error to the user.",
    inputSchema: {
      type: "object", additionalProperties: false, required: ["port"],
      properties: { port: { type: "integer", minimum: 1, maximum: 65535 } },
    },
  };
}
