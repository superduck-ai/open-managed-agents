export type Json =
  | null
  | boolean
  | number
  | string
  | Json[]
  | { [key: string]: Json };
export type Payload = Record<string, any>;

export function object(value: unknown): Payload {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Expected a JSON object");
  return value as Payload;
}
