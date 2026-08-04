import { randomBytes, timingSafeEqual } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { onlyCookie } from "../http/cookies";

const csrfCookieName = "vastplan_csrf";
const csrfTokenPattern = /^[a-f0-9]{64}$/;

/** Reuses and renews the browser's valid double-submit token so concurrent CSRF reads cannot invalidate each other. */
export function issueCSRF(request: IncomingMessage, response: ServerResponse, secure: boolean): string {
  const existing = onlyCookie(request, csrfCookieName);
  const token = existing !== undefined && csrfTokenPattern.test(existing) ? existing : randomBytes(32).toString("hex");
  const attributes = [`${csrfCookieName}=${token}`, "Path=/", "Max-Age=900", "SameSite=Strict"];
  if (secure) attributes.push("Secure");
  response.setHeader("Set-Cookie", attributes.join("; "));
  response.setHeader("Cache-Control", "no-store");
  return token;
}

export function validCSRF(request: IncomingMessage): boolean {
  const cookie = onlyCookie(request, csrfCookieName);
  const headerValue = request.headers["x-vastplan-csrf"];
  const header = Array.isArray(headerValue) ? undefined : headerValue;
  if (cookie === undefined || header === undefined || cookie.length !== header.length) return false;
  return timingSafeEqual(Buffer.from(cookie), Buffer.from(header));
}
