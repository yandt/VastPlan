export type FrontendContentKind = "entry" | "graph-node";

/** 集中定义前端加载链中所有随运行环境变化的规则；调用方不得再判断环境标记。 */
export interface FrontendRuntimeProtocol {
  readonly id: "production" | "development";
  governedDigest(url: string, kind: FrontendContentKind): string | undefined;
  requestCache(url: string): RequestCache;
  runtimeRetryDelay(status: number, retry: number): number | undefined;
  resolveCandidate<T>(exact: T | undefined, sameID: readonly T[]): T | undefined;
}

const productionEntryPatterns = [
  /^\/v1\/portal-modules\/[1-9]\d*\/([a-f0-9]{64})\.js$/,
  /^\/v1\/portal-recovery-modules\/[1-9]\d*\/[1-9]\d*\/([a-f0-9]{64})\.js$/,
];
const productionGraphPatterns = [
  /^\/v1\/portal-modules\/[1-9]\d*\/([a-f0-9]{64})\.(?:js|css|json|wasm|bin)$/,
  /^\/v1\/portal-recovery-modules\/[1-9]\d*\/[1-9]\d*\/([a-f0-9]{64})\.(?:js|css|json|wasm|bin)$/,
];
const developmentEntryPattern = /^\/__vastplan_dev\/modules\/([a-f0-9]{64})\.js$/;
const developmentGraphPattern = /^\/__vastplan_dev\/modules\/([a-f0-9]{64})\.(?:js|css|json|wasm|bin)$/;

function productionDigest(url: string, kind: FrontendContentKind): string | undefined {
  const patterns = kind === "entry" ? productionEntryPatterns : productionGraphPatterns;
  for (const pattern of patterns) {
    const digest = pattern.exec(url)?.[1];
    if (digest !== undefined) return digest;
  }
  return undefined;
}

export const productionFrontendRuntimeProtocol: FrontendRuntimeProtocol = Object.freeze({
  id: "production",
  governedDigest: productionDigest,
  requestCache: () => "force-cache",
  runtimeRetryDelay: () => undefined,
  resolveCandidate<T>(exact: T | undefined): T | undefined { return exact; },
});

export const developmentFrontendRuntimeProtocol: FrontendRuntimeProtocol = Object.freeze({
  id: "development",
  governedDigest(url: string, kind: FrontendContentKind) {
    const governed = productionDigest(url, kind);
    if (governed !== undefined) return governed;
    return (kind === "entry" ? developmentEntryPattern : developmentGraphPattern).exec(url)?.[1];
  },
  requestCache: (url: string) => url.startsWith("/__vastplan_dev/modules/") ? "no-store" : "force-cache",
  runtimeRetryDelay(status: number, retry: number): number | undefined {
    if (![502, 503, 504].includes(status) || retry >= 18) return undefined;
    return Math.min(250 * (2 ** Math.min(retry, 3)), 2_000);
  },
  resolveCandidate<T>(exact: T | undefined, sameID: readonly T[]): T | undefined {
    return exact ?? (sameID.length === 1 ? sameID[0] : undefined);
  },
});
