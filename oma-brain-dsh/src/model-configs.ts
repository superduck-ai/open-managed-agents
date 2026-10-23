import type { LlmCallConfig, LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";
import type { Assignment } from "./assignment.ts";

export interface ModelConfig {
  context_window?: number;
  max_tokens?: number;
}

export function validateModelConfigs(value: unknown): asserts value is Record<string, ModelConfig> | undefined {
  if (value === undefined) return;
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid model configs");
  for (const [model, entry] of Object.entries(value)) {
    if (!model || model !== model.trim() || !entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Invalid model config");
    const config = entry as Record<string, unknown>;
    if (Object.keys(config).some(key => key !== "context_window" && key !== "max_tokens")) throw new Error("Invalid model config fields");
    for (const limit of Object.values(config))
      if (typeof limit !== "number" || !Number.isSafeInteger(limit) || limit <= 0) throw new Error("Invalid model token limit");
    const { context_window: contextWindow, max_tokens: maxTokens } = config as ModelConfig;
    if (contextWindow !== undefined && maxTokens !== undefined && maxTokens >= contextWindow) throw new Error("Invalid model output budget");
  }
}

export function modelConfig(assignment: Assignment, model: string): ModelConfig | undefined {
  const configs = assignment.launch.model_configs;
  return configs && Object.hasOwn(configs, model) ? configs[model] : undefined;
}

export function omaModelInfo(assignment: Assignment, provider: string, model: string): LlmResolvedModelInfo {
  const config = modelConfig(assignment, model);
  return { provider, id: model, name: model,
    ...(config?.context_window === undefined ? {} : { context: { contextWindow: config.context_window } }),
    ...(config?.max_tokens === undefined ? {} : { defaultMaxTokens: config.max_tokens }) };
}

export function configureOmaModel(assignment: Assignment, provider: string, call: LlmCallConfig): LlmCallConfig {
  const configured = { ...call, provider };
  delete configured.maxTokens;
  const maxTokens = modelConfig(assignment, call.model)?.max_tokens;
  if (maxTokens !== undefined) configured.maxTokens = maxTokens;
  return configured;
}
