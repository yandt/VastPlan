import { createHash } from "node:crypto";
import { join } from "node:path";

export function snapshotPath(root: string, tenantId: string, portalId: string, revision: number): string {
  const key = createHash("sha256").update(tenantId).update("\0").update(portalId).digest("hex");
  return join(root, "snapshots", key, `${revision}.json`);
}

export function objectPath(root: string, digest: string, gzip = false): string {
  return join(root, "objects", digest.slice(0, 2), `${digest}.blob${gzip ? ".gz" : ""}`);
}
