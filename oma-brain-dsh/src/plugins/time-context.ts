import type { Context } from "@deepseek-ai/cordis";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import * as TimeContext from "@deepseek-ai/dsh-time-context";

export const name = "oma-time-context";
export const inject = ["agents", "sessionProjections"];
export const Config = TimeContext.Config;

function wrap(text: string): string {
  return "\n\n<system-reminder>\n"
    + "Runtime time context, separate from the user's message. "
    + "Use when relevant; exclude from text the user asks you to copy or transform.\n\n"
    + text + "\n</system-reminder>\n\n";
}

function formatMessage(message: UserMessage): UserMessage {
  const source = message.source;
  if (source.kind !== "time-context" || source.form !== "snapshot") return message;
  return {
    ...message,
    content: message.content.map(block => block.type === "text" ? { ...block, text: wrap(block.text) } : block),
    source: { ...source, sections: source.sections.map(section => ({ ...section, text: wrap(section.text) })) },
  };
}

export async function apply(ctx: Context, config: TimeContext.Config): Promise<void> {
  await ctx.plugin(TimeContext, config);
  ctx.on("agent/pre-step", async ({ signal }, next) => {
    const decision = await next();
    if (decision.kind === "reject" || signal.aborted) return decision;
    return { ...decision, messages: decision.messages.map(formatMessage) };
  }, { prepend: true });
}
