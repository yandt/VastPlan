import type { ServerResponse } from "node:http";

export function sendJSON(response: ServerResponse, status: number, value: unknown, head = false): void {
  response.statusCode = status;
  response.setHeader("Content-Type", "application/json; charset=utf-8");
  if (head) response.end();
  else response.end(`${JSON.stringify(value)}\n`);
}

export function sendAPIError(response: ServerResponse, status: number, code: string, head = false): void {
  sendJSON(response, status, { error: code }, head);
}
