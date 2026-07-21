import type { ServerResponse } from "node:http";

export function setBaseSecurityHeaders(response: ServerResponse, secure: boolean): void {
  response.setHeader("X-Content-Type-Options", "nosniff");
  response.setHeader("Referrer-Policy", "same-origin");
  response.setHeader("X-Frame-Options", "DENY");
  response.setHeader("Cross-Origin-Opener-Policy", "same-origin");
  response.setHeader("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()");
  response.setHeader("Cache-Control", "no-store");
  if (secure) response.setHeader("Strict-Transport-Security", "max-age=31536000");
}

export function setIndexSecurityHeaders(response: ServerResponse, nonce: string): void {
  response.setHeader(
    "Content-Security-Policy",
    `default-src 'self'; script-src 'self' blob: 'nonce-${nonce}'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; worker-src 'none'`,
  );
  response.setHeader("Content-Type", "text/html; charset=utf-8");
  response.setHeader("Cache-Control", "no-store");
}
