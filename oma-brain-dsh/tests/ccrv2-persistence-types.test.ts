import { expectTypeOf, test } from "bun:test";
import type { CcrBackend } from "../src/ccrv2/contracts.ts";
import type { CcrClient } from "../src/ccrv2/client.ts";
import type { DshLogData } from "../src/ccrv2/persistence-contracts.ts";
import type { SessionProjection } from "../src/ccrv2/protocol.ts";

type Accepts<Args extends unknown[]> = Args extends Parameters<CcrBackend["persist"]> ? true : false;

test("persistence rejects unknown subtypes, incomplete data and aggregate projections", () => {
  expectTypeOf<Accepts<[string, "unknown", DshLogData]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", unknown]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", {}]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_brain_state", SessionProjection]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", SessionProjection]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_brain_state", DshLogData]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log" | "dsh_brain_state", DshLogData]>>().toEqualTypeOf<false>();
});

test("persistence accepts native logs and preserves the client and backend contract", () => {
  expectTypeOf<Accepts<[string, "dsh_log", DshLogData]>>().toEqualTypeOf<true>();
  expectTypeOf<Accepts<[string, "dsh_log", DshLogData, string]>>().toEqualTypeOf<true>();
  expectTypeOf<Parameters<CcrClient["persist"]>>().toEqualTypeOf<Parameters<CcrBackend["persist"]>>();
});
