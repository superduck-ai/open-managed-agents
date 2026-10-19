import { LlmError, type GenerateOptions } from "@deepseek-ai/dsh-llm";

interface ChatMessage {
  role: "system" | "developer" | "user" | "assistant" | "tool";
  content: string | null;
  tool_calls?: { id: string; type: "function"; function: { name: string; arguments: string } }[];
  tool_call_id?: string;
}

export function chatRequest(options: GenerateOptions) {
  const messages: ChatMessage[] = [];
  if (options.system) messages.push({ role: "system", content: options.system });
  for (const message of options.messages) {
    const text: string[] = [];
    const calls: NonNullable<ChatMessage["tool_calls"]> = [];
    for (const block of message.content) {
      if (block.type === "text") text.push(block.text);
      else if (block.type === "tool-call" && message.role === "assistant") {
        calls.push({ id: block.id, type: "function", function: { name: block.name, arguments: block.arguments } });
      } else throw new LlmError(`Unsupported Chat Completions input block: ${block.type}`, "INVALID_ARGS");
    }
    if (message.role === "tool") {
      messages.push({ role: "tool", tool_call_id: message.source.callId, content: text.join("\n") });
    } else {
      messages.push({ role: message.role, content: calls.length ? text.join("\n") || null : text.join("\n"),
        ...(calls.length ? { tool_calls: calls } : {}) });
    }
  }
  return {
    model: options.model, messages, stream: true, n: 1,
    stream_options: { include_usage: true },
    ...(options.maxTokens !== undefined ? { max_completion_tokens: options.maxTokens } : {}),
    ...(options.temperature !== undefined ? { temperature: options.temperature } : {}),
    ...(options.stop?.length ? { stop: options.stop } : {}),
    ...(options.tools?.length ? { tools: options.tools.map((tool) => ({ type: "function", function: {
      name: tool.name, description: tool.description, parameters: tool.parameters,
    } })) } : {}),
  };
}
