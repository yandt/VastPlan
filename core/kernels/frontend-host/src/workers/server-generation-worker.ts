import { parentPort, workerData } from "node:worker_threads";
import { pathToFileURL } from "node:url";
import { validateFrontendServerRuntime, type FrontendServerRuntime } from "@vastplan/frontend-engine-contract";
import type { ServerWorkerData, ServerWorkerRequest, ServerWorkerResponse } from "./server-generation-contract";

const port = parentPort;
if (port === null) throw new Error("Server Generation Worker 必须由 Portal Kernel 启动");
const data = workerData as Partial<ServerWorkerData>;
if (typeof data.entryPath !== "string" || data.entryPath === "") throw new Error("Server Generation Worker 缺少入口路径");
const entryPath = data.entryPath;

let runtime: FrontendServerRuntime | undefined;

port.on("message", (request: ServerWorkerRequest) => {
  void handle(request).then(
    (result) => port.postMessage({ id: request.id, ok: true, ...(result === undefined ? {} : { result }) } satisfies ServerWorkerResponse),
    (error: unknown) => port.postMessage({ id: request.id, ok: false, error: error instanceof Error ? error.message : String(error) } satisfies ServerWorkerResponse),
  );
});

async function handle(request: ServerWorkerRequest) {
  if (!Number.isSafeInteger(request.id) || request.id < 1) throw new Error("Server Worker 请求 ID 无效");
  const signal = AbortSignal.timeout(request.operation === "render" ? 5_000 : 10_000);
  if (request.operation === "prepare") {
    if (runtime !== undefined) return;
    const loaded = await import(`${pathToFileURL(entryPath).href}?worker=${Date.now()}`);
    runtime = validateFrontendServerRuntime(loaded.default);
    await runtime.prepare?.(signal);
    return;
  }
  if (runtime === undefined) throw new Error("Server Runtime 尚未 prepare");
  if (request.operation === "dispose") {
    await runtime.dispose?.();
    runtime = undefined;
    return;
  }
  if (request.operation !== "render" || request.input === undefined) throw new Error("Server Worker 操作无效");
  return runtime.render(request.input, signal);
}
