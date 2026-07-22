import type { AccessGeneration } from "./access-generation";

export interface AccessCatalogPort {
  resolve(host: string, path: string): Promise<AccessGeneration | undefined>;
}
