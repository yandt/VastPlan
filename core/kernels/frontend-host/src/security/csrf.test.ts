import { describe, expect, it } from "vitest";
import type { IncomingMessage, ServerResponse } from "node:http";
import { issueCSRF, renewCSRF } from "./csrf";

describe("CSRF double-submit token", () => {
  it("extends the same token after a successful protected request", () => {
    const issued = response();
    const token = issueCSRF(request({}), issued.value, false);
    const renewed = response();

    expect(renewCSRF(request({ cookie: `vastplan_csrf=${token}`, "x-vastplan-csrf": token }), renewed.value, false)).toBe(true);
    expect(renewed.headers.get("Set-Cookie")).toBe(`vastplan_csrf=${token}; Path=/; Max-Age=900; SameSite=Strict`);
    expect(renewed.headers.get("Cache-Control")).toBe("no-store");
  });

  it("does not renew a mismatched token", () => {
    const renewed = response();

    expect(renewCSRF(request({ cookie: `vastplan_csrf=${"a".repeat(64)}`, "x-vastplan-csrf": "b".repeat(64) }), renewed.value, false)).toBe(false);
    expect(renewed.headers.size).toBe(0);
  });
});

function request(headers: Record<string, string>): IncomingMessage {
  return { headers } as IncomingMessage;
}

function response(): { value: ServerResponse; headers: Map<string, number | string | readonly string[]> } {
  const headers = new Map<string, number | string | readonly string[]>();
  return {
    headers,
    value: {
      getHeader(name: string): number | string | string[] | undefined { return headers.get(name) as number | string | string[] | undefined; },
      setHeader(name: string, value: number | string | readonly string[]): ServerResponse { headers.set(name, value); return this as ServerResponse; },
    } as unknown as ServerResponse,
  };
}
