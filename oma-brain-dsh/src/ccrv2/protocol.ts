import type { Payload } from "../json.ts";
export { object, type Json, type Payload } from "../json.ts";

const controlInputTypes = new Set(["control_response", "control_request", "interrupt", "user.interrupt", "keep_alive"]);
export const isControlInput = (type: unknown): boolean => typeof type === "string" && controlInputTypes.has(type);
