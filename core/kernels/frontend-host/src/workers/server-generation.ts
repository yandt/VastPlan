import { Worker } from "node:worker_threads";
import type { FrontendServerRenderInput, FrontendServerRenderResult } from "@vastplan/frontend-engine-contract";
import { validateWorkerRenderResult, type ServerWorkerOperation, type ServerWorkerResponse } from "./server-generation-contract";

interface PendingRequest {
  readonly resolve: (value: unknown) => void;
  readonly reject: (error: Error) => void;
  readonly timer: NodeJS.Timeout;
}

export class ServerGeneration {
  private readonly worker: Worker;
  private readonly pending = new Map<number, PendingRequest>();
  private nextID = 1;
  private activeRenders = 0;
  private accepting = true;
  private drainResolve?: () => void;
	private disposal?: Promise<void>;

  private constructor(public readonly key: string, workerScript: string, entryPath: string) {
    this.worker = new Worker(workerScript, {
      workerData: { entryPath },
      resourceLimits: { maxOldGenerationSizeMb: 256, maxYoungGenerationSizeMb: 32, stackSizeMb: 4 },
    });
    this.worker.on("message", (response: ServerWorkerResponse) => this.receive(response));
    this.worker.on("error", (error) => this.fail(error));
    this.worker.on("exit", (code) => { if (code !== 0 && this.pending.size > 0) this.fail(new Error(`SSR Worker 异常退出: ${code}`)); });
  }

  public static async start(key: string, workerScript: string, entryPath: string): Promise<ServerGeneration> {
    const generation = new ServerGeneration(key, workerScript, entryPath);
    await generation.invoke("prepare", undefined, 10_000);
    return generation;
  }

  public async render(input: FrontendServerRenderInput): Promise<FrontendServerRenderResult> {
    if (!this.accepting) throw new Error("Server Generation 已进入 drain");
    this.activeRenders += 1;
    try { return validateWorkerRenderResult(await this.invoke("render", input, 5_000)); }
    finally {
      this.activeRenders -= 1;
      if (this.activeRenders === 0) this.drainResolve?.();
    }
  }

  public async dispose(): Promise<void> {
		this.disposal ??= this.performDispose();
		return this.disposal;
  }

	private async performDispose(): Promise<void> {
		this.accepting = false;
		if (this.activeRenders > 0) await new Promise<void>((resolve) => { this.drainResolve = resolve; });
		try { await this.invoke("dispose", undefined, 5_000); }
		finally { await this.worker.terminate(); }
	}

  private invoke(operation: ServerWorkerOperation, input: FrontendServerRenderInput | undefined, timeout: number): Promise<unknown> {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`SSR Worker ${operation} 超时`));
        void this.worker.terminate();
      }, timeout);
      timer.unref();
      this.pending.set(id, { resolve, reject, timer });
      this.worker.postMessage({ id, operation, ...(input === undefined ? {} : { input }) });
    });
  }

  private receive(response: ServerWorkerResponse): void {
    const pending = this.pending.get(response.id);
    if (pending === undefined) return;
    this.pending.delete(response.id);
    clearTimeout(pending.timer);
    if (response.ok) pending.resolve(response.result);
    else pending.reject(new Error(response.error ?? "SSR Worker 执行失败"));
  }

  private fail(error: Error): void {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }
}
