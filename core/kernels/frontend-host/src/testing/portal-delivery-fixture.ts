import { createHash } from "node:crypto";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { gzipSync } from "node:zlib";
import { objectPath, snapshotPath } from "../runtime/portal-delivery-paths";
import { portalSpecDigest, type PortalRuntimeSpec, type PortalSpec } from "../runtime/portal-runtime-contract";

export interface PortalDeliveryFixture {
  readonly cache: string;
  readonly origin: string;
}

export interface PortalDeliveryRevision {
  readonly spec: PortalSpec;
  readonly runtime: PortalRuntimeSpec;
  readonly content: Uint8Array;
  readonly digest: string;
}

export async function createPortalDeliveryFixture(): Promise<PortalDeliveryFixture> {
  const root = await mkdtemp(join(tmpdir(), "vastplan-delivery-"));
  return { cache: join(root, "cache"), origin: join(root, "origin") };
}

export async function writePortalDeliveryRevision(fixture: PortalDeliveryFixture, spec: PortalSpec, source = "export const ready = true;\n"): Promise<PortalDeliveryRevision> {
  const content = new TextEncoder().encode(source);
  const digest = createHash("sha256").update(content).digest("hex");
  const runtime: PortalRuntimeSpec = {
    portal: spec,
    modules: [{
      id: "cn.vastplan.example", version: "1.0.0", channel: "stable", entry: "frontend/dist/index.js",
      url: `/v1/portal-modules/${spec.revision}/${digest}.js`, sha256: digest,
      packageSha256: "b".repeat(64), mediaType: "text/javascript",
    }],
  };
  const snapshot = JSON.stringify({ specSha256: portalSpecDigest(spec), runtime });
  await mkdir(join(fixture.origin, "objects", digest.slice(0, 2)), { recursive: true });
  await mkdir(join(fixture.origin, "snapshots", createHash("sha256").update(`${spec.tenantId}\0${spec.id}`).digest("hex")), { recursive: true });
  await writeFile(objectPath(fixture.origin, digest), content);
  await writeFile(objectPath(fixture.origin, digest, true), gzipSync(content));
  await writeFile(snapshotPath(fixture.origin, spec.tenantId, spec.id, spec.revision), snapshot);
  return { spec, runtime, content, digest };
}
