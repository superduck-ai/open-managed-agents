import type { OmaApi } from "../api.ts";
import type { Transport } from "./http.ts";

export function createDsrTransport(api: OmaApi): Transport {
  return {
    request: options => api.requestResponse(options.path, options.token, options.method, options.body,
      options.headers, options.signal, options.timeout),
  };
}
