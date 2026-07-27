import type { PortalRuntimeSpec } from "./module-runtime-spec";
import type { FrontendRuntimeProtocol } from "./frontend-runtime-protocol";

/** 首次启动与后续运行代共同使用的 Runtime 输入协议。 */
export interface PortalRuntimeSource {
  readonly protocol: FrontendRuntimeProtocol;
  read(pathname: string): Promise<PortalRuntimeSpec>;
}
