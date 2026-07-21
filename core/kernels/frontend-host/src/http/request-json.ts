import type { IncomingMessage, ServerResponse } from "node:http";
import { sendAPIError } from "./json-response";

const defaultMaximumBodyBytes = 1 << 20;

export class RequestJSONError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "RequestJSONError";
  }
}

export async function readRequestJSON(request: IncomingMessage, maximumBytes = defaultMaximumBodyBytes): Promise<unknown> {
  const declared = Number(request.headers["content-length"]);
  if (Number.isFinite(declared) && declared > maximumBytes) throw new RequestJSONError("请求体超过上限");
  const chunks: Buffer[] = [];
  let size = 0;
  try {
    for await (const chunk of request) {
      const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as Uint8Array);
      size += bytes.byteLength;
      if (size > maximumBytes) throw new RequestJSONError("请求体超过上限");
      chunks.push(bytes);
    }
    if (size === 0) throw new RequestJSONError("请求体不能为空");
    return JSON.parse(Buffer.concat(chunks, size).toString("utf8")) as unknown;
  } catch (error) {
    if (error instanceof RequestJSONError) throw error;
    throw new RequestJSONError("请求 JSON 无效");
  }
}

export function requireJSONObject(value: unknown): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new RequestJSONError("请求 JSON 必须是对象");
  return value as Readonly<Record<string, unknown>>;
}

export async function withRequestJSON(request: IncomingMessage, response: ServerResponse, action: (value: unknown) => Promise<void>): Promise<void> {
  try { await action(await readRequestJSON(request)); }
  catch (error) {
    if (error instanceof RequestJSONError) sendAPIError(response, 400, "invalid_json");
    else throw error;
  }
}
