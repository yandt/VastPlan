import type { IncomingMessage } from "node:http";

const maximumQueryKeys = 64;
const maximumQueryValues = 32;
const maximumQueryValueBytes = 4_096;

export class APIBodyTooLargeError extends Error {}
export class APIUnsupportedMediaTypeError extends Error {}

export async function readAPIJSONBody(request: IncomingMessage, maximumBytes: number, method: string): Promise<unknown> {
  const contentLength = request.headers["content-length"];
  const declared = contentLength === undefined ? undefined : Number(contentLength);
  if (declared !== undefined && (!Number.isSafeInteger(declared) || declared < 0)) throw new Error("Content-Length 无效");
  if (declared !== undefined && declared > maximumBytes) throw new APIBodyTooLargeError();
  if ((method === "GET" || method === "HEAD") && ((declared ?? 0) > 0 || request.headers["transfer-encoding"] !== undefined)) throw new Error(`${method} 不得包含请求体`);
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as Uint8Array);
    size += bytes.byteLength;
    if (size > maximumBytes) throw new APIBodyTooLargeError();
    chunks.push(bytes);
  }
  if (size === 0) return {};
  const contentType = request.headers["content-type"]?.split(";", 1)[0].trim().toLowerCase();
  if (contentType !== "application/json") throw new APIUnsupportedMediaTypeError();
  try { return JSON.parse(Buffer.concat(chunks, size).toString("utf8")) as unknown; }
  catch { throw new Error("请求 JSON 无效"); }
}

export function parseAPIQuery(rawURL: string): Readonly<Record<string, readonly string[]>> {
  const url = new URL(rawURL, "https://gateway.invalid");
  const result: Record<string, string[]> = {};
  for (const [key, value] of url.searchParams) {
    if (!/^[a-z][A-Za-z0-9._-]*$/.test(key) || Buffer.byteLength(key) > 160 || Buffer.byteLength(value) > maximumQueryValueBytes) throw new Error("query 超过上限");
    const values = result[key] ?? [];
    if (values.length >= maximumQueryValues) throw new Error("query 重复值超过上限");
    values.push(value);
    result[key] = values;
  }
  if (Object.keys(result).length > maximumQueryKeys) throw new Error("query key 超过上限");
  return Object.freeze(Object.fromEntries(Object.entries(result).map(([key, values]) => [key, Object.freeze(values)])));
}
