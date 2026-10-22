import type { Context } from "@deepseek-ai/cordis";
import { defineTool, validateJsonSchemaValue, valueSchemaSpecToJsonSchema, type InferArgs, type InferValue } from "@deepseek-ai/dsh-tools";
import { approveToolCall } from "../runtime/tool-approvals.ts";
import type { ToolExecutionBridge } from "../runtime/contracts.ts";
import { toolText } from "../runtime/tools.ts";

export const name = "oma-user-questions";
export const inject = ["tools"];

export interface Config {
  bridge: ToolExecutionBridge;
  tools?: string[];
}

const parameters = {
  questions: { type: "array", required: true, items: {
    type: "object", additionalProperties: true, properties: {
      id: { type: "string", required: true, description: "Stable unique question id, echoed in the answer." },
      question: { type: "string", required: true, description: "The specific question to ask the user." },
      header: { type: "string", description: "Optional short heading for the question." },
      options: { type: "array", description: "Optional choices. Put a recommended choice first and append (Recommended) to its label.", items: {
        type: "object", additionalProperties: true, properties: { label: { type: "string", required: true }, description: { type: "string" } },
      } },
      multi_select: { type: "boolean", description: "Whether more than one option may be selected. Defaults to false." },
    },
  } },
} as const;

const output = {
  type: "object", additionalProperties: false, properties: { answers: { type: "array", required: true, items: {
    type: "object", additionalProperties: false, properties: {
      id: { type: "string", required: true }, selected: { type: "array", required: true, items: { type: "string" } }, custom: { type: "string" },
    },
  } } },
} as const;
const answerSchema = valueSchemaSpecToJsonSchema(output);

export function apply(ctx: Context, config: Config): void {
  if (!config.tools?.includes("ask_user_question")) return;
  ctx.tools.register(defineTool({
    name: "AskUserQuestion",
    description: "Ask the user a concise question when you need confirmation, a choice, or missing information before proceeding. Wait for their answer before continuing.",
    parameters,
    output: { schema: output, render: (_args, value) => [{ type: "text", text: JSON.stringify(value) }] },
    execute: async (args, exec) => {
      if (!args.questions.length || args.questions.some(question => !question.id || !question.question)) throw new Error("Questions require non-empty ids and text");
      if (new Set(args.questions.map(question => question.id)).size !== args.questions.length) throw new Error("Question ids must be unique");
      const decision = await approveToolCall(config.bridge, {
        callId: exec.callId, toolName: "AskUserQuestion", input: args, available: true,
      }, exec.signal);
      exec.signal.throwIfAborted();
      if (!decision.allowed) throw new Error(decision.reason || "User question was denied");
      if (!decision.externalResult) throw new Error("Missing user question result");
      const text = toolText(decision.externalResult);
      if (decision.externalResult.isError) throw new Error(text || "User question was cancelled");
      return questionAnswers(args.questions, JSON.parse(text));
    },
  }));
}

export function questionAnswers(questions: InferArgs<typeof parameters>["questions"], value: unknown): InferValue<typeof output> {
  if (validateJsonSchemaValue(answerSchema, value).length) throw new Error("Invalid user question answers");
  const result = value as InferValue<typeof output>;
  const byId = new Map(result.answers.map(answer => [answer.id, answer]));
  if (result.answers.length !== questions.length || byId.size !== questions.length) throw new Error("Answers must match each question exactly once");
  for (const question of questions) {
    const answer = byId.get(question.id);
    if (!answer) throw new Error("Answer references an unknown question id");
    if (new Set(answer.selected).size !== answer.selected.length) throw new Error("Selected options must be unique");
    const labels = new Set(question.options?.map(option => option.label));
    if (answer.selected.some(label => !labels.has(label))) throw new Error("Answer contains an unknown option");
    if (!question.multi_select && (answer.selected.length > 1 || answer.selected.length > 0 && answer.custom))
      throw new Error("Single-select question allows at most one option or a custom answer, not both");
  }
  return result;
}
