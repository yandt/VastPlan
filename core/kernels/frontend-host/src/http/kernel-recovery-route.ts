import type { IncomingMessage, ServerResponse } from "node:http";
import { sendAPIError, sendJSON } from "./json-response";

const stageIDs = new Set(["recovery", "control-plane", "platform"]);
const statuses = new Set(["Pending", "Ready", "Degraded", "Failed"]);
const overallStatuses = new Set(["Starting", "RecoveryReady", "ControlPlaneReady", "PlatformReady"]);

export interface PublicKernelRecoveryStatus {
  schemaVersion: 1;
  overall: string;
  scope: "local" | "cluster";
  clusterAvailable: boolean;
  nodes: number;
  stages: readonly { id: string; status: string; ready: number; required: number }[];
  updatedAt: string;
}

export class KernelRecoveryClient {
  public constructor(private readonly baseURL: string, private readonly fetcher: typeof fetch = fetch) {}

  public async status(signal?: AbortSignal): Promise<PublicKernelRecoveryStatus> {
    const response = await this.fetcher(new URL("/v1/recovery/status", this.baseURL), { method: "GET", signal });
    if (!response.ok) throw new Error(`kernel recovery unavailable (${response.status})`);
    return parsePublicStatus(await response.json());
  }
}

export async function serveKernelRecovery(client: KernelRecoveryClient, request: IncomingMessage, response: ServerResponse): Promise<void> {
  const method = request.method ?? "GET";
  response.setHeader("Cache-Control", "no-store");
  if (method !== "GET" && method !== "HEAD") return sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2_000);
  try {
    const status = await client.status(controller.signal);
    sendJSON(response, 200, status, method === "HEAD");
  } catch {
    sendAPIError(response, 503, "kernel_recovery_unavailable", method === "HEAD");
  } finally {
    clearTimeout(timeout);
  }
}

export async function serveKernelRecoveryPage(client: KernelRecoveryClient, request: IncomingMessage, response: ServerResponse): Promise<void> {
  const method = request.method ?? "GET";
  response.setHeader("Cache-Control", "no-store");
  response.setHeader("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'");
  if (method !== "GET" && method !== "HEAD") {
    response.setHeader("Allow", "GET, HEAD");
    response.statusCode = 405;
    response.end();
    return;
  }
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2_000);
  let body: string;
  try {
    const status = await client.status(controller.signal);
    const stages = status.stages.map((stage) => `<li><strong>${stageName(stage.id)}</strong>: ${stage.status} (${stage.ready}/${stage.required})</li>`).join("");
    body = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>VastPlan Recovery</title><main><p>VASTPLAN SAFE MODE</p><h1>平台恢复状态</h1><p>${status.overall} · ${status.scope} · ${status.nodes} 个可信节点</p><ol>${stages}</ol><p><a href="/recovery">刷新状态</a> · <a href="/operations">返回平台</a></p></main></html>`;
  } catch {
    response.statusCode = 503;
    body = "<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><title>VastPlan Recovery</title><main><p>VASTPLAN SAFE MODE</p><h1>恢复状态暂不可用</h1><p>请检查 Backend Kernel、Node Agent 与本机 Recovery 端点。</p></main></html>";
  } finally {
    clearTimeout(timeout);
  }
  response.setHeader("Content-Type", "text/html; charset=utf-8");
  if (response.statusCode === 200 || response.statusCode === 0) response.statusCode = 200;
  if (method === "HEAD") response.end();
  else response.end(body);
}

function parsePublicStatus(value: unknown): PublicKernelRecoveryStatus {
  if (!isRecord(value) || value.schemaVersion !== 1 || typeof value.overall !== "string" || !overallStatuses.has(value.overall) || value.scope !== "local" && value.scope !== "cluster" || typeof value.clusterAvailable !== "boolean" || !safeCount(value.nodes) || typeof value.updatedAt !== "string" || Number.isNaN(Date.parse(value.updatedAt)) || !Array.isArray(value.stages) || value.stages.length !== 3) {
    throw new Error("kernel recovery response invalid");
  }
  const expectedStages = ["recovery", "control-plane", "platform"] as const;
  const stages = value.stages.map((stage, index) => {
    if (!isRecord(stage) || typeof stage.id !== "string" || !stageIDs.has(stage.id) || stage.id !== expectedStages[index] || typeof stage.status !== "string" || !statuses.has(stage.status) || !safeCount(stage.ready) || !safeCount(stage.required) || stage.required === 0 || stage.ready > stage.required) {
      throw new Error("kernel recovery stage invalid");
    }
    return Object.freeze({ id: stage.id, status: stage.status, ready: stage.ready, required: stage.required });
  });
  return Object.freeze({ schemaVersion: 1, overall: value.overall, scope: value.scope, clusterAvailable: value.clusterAvailable, nodes: value.nodes, stages: Object.freeze(stages), updatedAt: value.updatedAt });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function safeCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= 1_000_000;
}

function stageName(id: string): string {
  if (id === "recovery") return "恢复基础";
  if (id === "control-plane") return "控制面";
  return "完整平台";
}
