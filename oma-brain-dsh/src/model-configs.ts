import type { LlmCallConfig, LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";
import type { Assignment, ModelConfig } from "./assignment.ts";
import { DEFAULT_CONTEXT_WINDOW, DEFAULT_MAX_OUTPUT_TOKENS } from "./model-defaults.ts";

function modelConfig(assignment: Assignment | undefined, model: string): ModelConfig | undefined {
  const models = assignment?.launch.models;
  return models && Object.hasOwn(models, model) ? models[model] : undefined;
}

export function omaModelInfo(assignment: Assignment | undefined, provider: string, model: string): LlmResolvedModelInfo {
  const config = modelConfig(assignment, model);
  return { provider, id: model, name: model,
    inputModalities: config?.input_modalities ?? ["text"],
    context: { contextWindow: config?.context_window ?? DEFAULT_CONTEXT_WINDOW },
    ...(config?.max_output_tokens === undefined ? {} : { defaultMaxTokens: config.max_output_tokens }) };
}

export function configureOmaModel(assignment: Assignment, provider: string, call: LlmCallConfig): LlmCallConfig {
  return { ...call, provider, maxTokens: modelConfig(assignment, call.model)?.max_output_tokens ?? DEFAULT_MAX_OUTPUT_TOKENS };
}
