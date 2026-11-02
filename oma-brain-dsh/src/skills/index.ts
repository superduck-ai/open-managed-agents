import type { Context } from "@deepseek-ai/cordis";
import SkillRegistry, { type SkillDefinition } from "@deepseek-ai/dsh-skill";
import * as ToolSkill from "@deepseek-ai/dsh-tool-skill";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { OmaApi } from "../api.ts";
import { parseSkillCatalog } from "./catalog.ts";

export const name = "oma-skills";

export interface Config {
  api: OmaApi;
  sessionId: string;
  threadId?: string;
  workDir: string;
}

export async function apply(ctx: Context, config: Config): Promise<void> {
  const scope = ctx.isolate("skills").isolate("workingDirectory");
  await scope.plugin({
    name: "oma-skill-working-directory",
    apply(bindings: Context) {
      bindings.provide("workingDirectory", {
        ensure: async (_agent: Agent, signal?: AbortSignal) => {
          signal?.throwIfAborted();
          return config.workDir;
        },
      });
    },
  });
  await scope.plugin(SkillRegistry);
  await scope.plugin({
    name: "oma-skill-provider",
    inject: ["skills"],
    apply(providerContext: Context) {
      providerContext.skills.registerProvider(control => ({
        name: "oma",
        async list(options) {
          const catalog = await loadCatalog(config, AbortSignal.any([control.signal, ...(options.signal ? [options.signal] : [])]));
          return catalog.map(candidate => ({ ...candidate, locator: candidate.path!.split("/").at(-2)! }));
        },
        async get(candidate, options) {
          const signal = AbortSignal.any([control.signal, ...(options.signal ? [options.signal] : [])]);
          const definitions = await loadCatalog(config, signal, candidate.locator as string);
          if (definitions.length !== 1) throw new Error("Skill is no longer available");
          return definitions[0]!.locator as SkillDefinition;
        },
      }));
    },
  });
  await scope.plugin(ToolSkill);
}

async function loadCatalog(config: Config, signal: AbortSignal, directory?: string) {
  signal.throwIfAborted();
  const query = directory === undefined ? "" : `?directory=${encodeURIComponent(directory)}`;
  const response = await config.api.request(
    `/v2/sessions/${encodeURIComponent(config.sessionId)}/skills${query}`,
    config.api.assignment(config.sessionId).ingress_token, "GET", undefined,
    config.threadId ? { "X-OMA-Session-Thread-ID": config.threadId } : undefined, signal,
  );
  signal.throwIfAborted();
  return parseSkillCatalog(response);
}
