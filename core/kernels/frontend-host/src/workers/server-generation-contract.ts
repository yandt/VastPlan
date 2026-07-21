import type { FrontendServerRenderInput, FrontendServerRenderResult } from "@vastplan/frontend-engine-contract";

export type ServerWorkerOperation = "prepare" | "render" | "dispose";

export interface ServerWorkerRequest {
  readonly id: number;
  readonly operation: ServerWorkerOperation;
  readonly input?: FrontendServerRenderInput;
}

export interface ServerWorkerResponse {
  readonly id: number;
  readonly ok: boolean;
  readonly result?: FrontendServerRenderResult;
  readonly error?: string;
}

export interface ServerWorkerData {
  readonly entryPath: string;
}

export function validateWorkerRenderResult(value: unknown): FrontendServerRenderResult {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("SSR Worker 返回值无效");
  const result = value as Readonly<Record<string, unknown>>;
  if (typeof result.html !== "string" || Buffer.byteLength(result.html) > 1 << 20 || /<script(?:\s|>)|<\/template(?:\s|>)/i.test(result.html)) {
    throw new Error("SSR Worker HTML 超过上限或包含脚本");
  }
  if (result.head !== undefined && (!Array.isArray(result.head) || result.head.length > 32 || result.head.some((item) => typeof item !== "string" || Buffer.byteLength(item) > 8192))) {
    throw new Error("SSR Worker head 无效");
  }
  return { html: result.html, ...(result.head === undefined ? {} : { head: Object.freeze([...(result.head as string[])]) }) };
}
