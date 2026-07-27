import type { PluginRef } from "./portal-contracts";
import { ModuleLoadError } from "./module-errors";
import type { FrontendModuleGraphDescriptor } from "./module-graph-contract";
import type { FrontendModuleDescriptor } from "./module-runtime-spec";
import type { FrontendRuntimeProtocol } from "./frontend-runtime-protocol";

export type FrontendModuleCandidate =
  | { readonly kind: "module"; readonly descriptor: FrontendModuleDescriptor }
  | { readonly kind: "graph"; readonly descriptor: FrontendModuleGraphDescriptor };

/** 独立于传输和内容校验解析插件身份。 */
export interface FrontendModuleResolver {
  resolve(ref: PluginRef): FrontendModuleCandidate | undefined;
}

/** 精确匹配与同 ID 投影的取舍全部委托给注入的 Runtime 协议。 */
export class RuntimeSpecModuleResolver implements FrontendModuleResolver {
  private readonly exact = new Map<string, FrontendModuleCandidate>();
  private readonly candidatesByID = new Map<string, FrontendModuleCandidate[]>();

  public constructor(
    modules: readonly FrontendModuleDescriptor[],
    graphs: readonly FrontendModuleGraphDescriptor[],
    private readonly protocol: FrontendRuntimeProtocol,
  ) {
    for (const descriptor of modules) this.record({ kind: "module", descriptor });
    for (const descriptor of graphs) this.record({ kind: "graph", descriptor });
  }

  public resolve(ref: PluginRef): FrontendModuleCandidate | undefined {
    return this.protocol.resolveCandidate(this.exact.get(pluginKey(ref)), this.candidatesByID.get(ref.id) ?? []);
  }

  private record(candidate: FrontendModuleCandidate): void {
    const descriptor = candidate.descriptor;
    const key = pluginKey(descriptor);
    if (this.exact.has(key)) {
      throw new ModuleLoadError("MODULE_CANDIDATE_DUPLICATE", `前端模块候选重复: ${key}`);
    }
    this.exact.set(key, candidate);
    this.candidatesByID.set(descriptor.id, [...(this.candidatesByID.get(descriptor.id) ?? []), candidate]);
  }
}

export function pluginKey(ref: PluginRef): string {
  return `${ref.id}@${ref.version}/${ref.channel ?? "stable"}`;
}
