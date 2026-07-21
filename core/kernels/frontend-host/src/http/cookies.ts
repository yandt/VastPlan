import type { IncomingMessage } from "node:http";

export function onlyCookie(request: IncomingMessage, name: string): string | undefined {
  const header = request.headers.cookie;
  if (header === undefined) return undefined;
  const headers = Array.isArray(header) ? header : [header];
  let found: string | undefined;
  for (const value of headers) {
    for (const item of value.split(";")) {
      const separator = item.indexOf("=");
      if (separator < 1 || item.slice(0, separator).trim() !== name) continue;
      const candidate = item.slice(separator + 1).trim();
      if (candidate === "" || found !== undefined || /[\u0000-\u001f\u007f]/.test(candidate)) return undefined;
      found = candidate;
    }
  }
  return found;
}
