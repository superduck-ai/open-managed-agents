import { expectTypeOf, test } from "bun:test";
import type { CcrBackend } from "../src/ccrv2/contracts.ts";
import type { CcrClient } from "../src/ccrv2/client.ts";
import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import type { DshLogData } from "../src/ccrv2/persistence-contracts.ts";
type UnrelatedPayload = { status: string };

type Accepts<Args extends unknown[]> = Args extends Parameters<CcrBackend["persist"]> ? true : false;

test("persistence rejects unknown types, incomplete data and mismatched type/data pairs", () => {
  expectTypeOf<Accepts<[string, "unknown", UnrelatedPayload]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", unknown]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_header", DshLogData]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", { sessionId: string; event: SessionEvent }]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", { header: SessionHeader; inherited: number }]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", {}]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "ccrv2_delivery", { revision: number }]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log", UnrelatedPayload]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "ccrv2_delivery", DshLogData]>>().toEqualTypeOf<false>();
  expectTypeOf<Accepts<[string, "dsh_log" | "ccrv2_delivery", DshLogData | UnrelatedPayload]>>().toEqualTypeOf<false>();
});

test("persistence accepts matching data and preserves the client and backend contract", () => {
  expectTypeOf<Accepts<[string, "dsh_log", SessionHeader]>>().toEqualTypeOf<true>();
  expectTypeOf<Accepts<[string, "dsh_log", DshLogData]>>().toEqualTypeOf<true>();
  expectTypeOf<Accepts<[string, "dsh_log", DshLogData, string]>>().toEqualTypeOf<true>();
  expectTypeOf<Accepts<[string, "ccrv2_delivery", UnrelatedPayload]>>().toEqualTypeOf<false>();
  expectTypeOf<Parameters<CcrClient["persist"]>>().toEqualTypeOf<Parameters<CcrBackend["persist"]>>();
});
