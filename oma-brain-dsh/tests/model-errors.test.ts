import { expect, test } from "bun:test";
import { APICallError } from "@ai-sdk/provider";
import { modelError } from "../src/llm-ai-sdk/stream.ts";

const observedRangeError = "<400> InternalError.Algo.InvalidParameter: Range of input length should be [1, 260096]";

function requestError(message: string, requestBodyValues: unknown, statusCode = 400): APICallError {
  return new APICallError({ message, url: "http://model.test/v1/messages", requestBodyValues, statusCode,
    responseBody: JSON.stringify({ error: { code: "InvalidParameter", message, param: null, type: null } }) });
}

test("observed provider input range rejection triggers bounded context overflow recovery", () => {
  for (const content of ["nonempty input", [{ type: "text", text: "nonempty input" }]]) {
    const error = modelError(requestError(observedRangeError, { messages: [{ role: "user", content }] }));
    expect(error).toMatchObject({ code: "CONTEXT_WINDOW_EXCEEDED", message: observedRangeError });
  }
});

test("ambiguous lower bounds and unrelated provider validation remain ordinary HTTP errors", () => {
  for (const body of [undefined, {}, { messages: [] }, { messages: [{ content: "" }] },
    { messages: [{ content: [{ type: "text", text: "" }] }] }, { messages: [{ content: [{ type: "image" }] }] }]) {
    expect(modelError(requestError(observedRangeError, body))).toMatchObject({ code: "HTTP_ERROR" });
  }
  for (const message of ["Range of max_tokens should be [1, 32768]", "Invalid messages format",
    "<400> InternalError.Algo.InvalidParameter: Range of input length should be [1, 0]"]) {
    expect(modelError(requestError(message, { messages: [{ content: "nonempty" }] }))).toMatchObject({ code: "HTTP_ERROR" });
  }
  expect(modelError(requestError(observedRangeError, { messages: [{ content: "nonempty" }] }, 503))).toMatchObject({ code: "SERVER" });
});
