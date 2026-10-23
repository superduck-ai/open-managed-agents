import { expectTypeOf, test } from "bun:test";
import type { CcrBackend } from "../src/ccrv2/contracts.ts";
import type { CcrClient } from "../src/ccrv2/client.ts";
import type { CcrPersistence } from "../src/ccrv2/persistence.ts";
import type { DshLogData } from "../src/ccrv2/persistence-contracts.ts";
import type { BrainState } from "../src/ccrv2/protocol.ts";

type Accepts<Args extends unknown[]> = Args extends Parameters<CcrBackend["persist"]> ? true : false;

test("persistence rejects unknown subtypes, incomplete data and mismatched subtype/data pairs", () => {
  expectTypeOf<Accepts<[string, "unknown", BrainState]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", unknown]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", {}]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_brain_state", { revision: number }]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", BrainState]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_brain_state", DshLogData]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log" | "dsh_brain_state", DshLogData | BrainState]>>().toEqualTypeOf<false>();
});

test("persistence accepts matching data and preserves the client and backend contract", () => {
  expectTypeOf<Accepts<[string, "dsh_log", DshLogData]>>().toEqualTypeOf<true>();
  expectTypeOf<Accepts<[string, "dsh_log", DshLogData, string]>>().toEqualTypeOf<true>();
  expectTypeOf<Accepts<[string, "dsh_brain_state", BrainState]>>().toEqualTypeOf<true>();
  expectTypeOf<Parameters<CcrClient["persist"]>>().toEqualTypeOf<Parameters<CcrBackend["persist"]>>();
  expectTypeOf<Parameters<CcrPersistence["persist"]>[1]>().toEqualTypeOf<DshLogData>();
});
