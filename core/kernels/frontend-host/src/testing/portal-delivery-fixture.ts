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
	readonly serverContent?: Uint8Array;
	readonly serverDigest?: string;
}

export async function createPortalDeliveryFixture(): Promise<PortalDeliveryFixture> {
  const root = await mkdtemp(join(tmpdir(), "vastplan-delivery-"));
  return { cache: join(root, "cache"), origin: join(root, "origin") };
}

export async function writePortalDeliveryRevision(fixture: PortalDeliveryFixture, spec: PortalSpec, source = "export const ready = true;\n", serverSource?: string): Promise<PortalDeliveryRevision> {
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
	const serverContent = serverSource === undefined ? undefined : new TextEncoder().encode(serverSource);
	const serverDigest = serverContent === undefined ? undefined : createHash("sha256").update(serverContent).digest("hex");
	const server = serverDigest === undefined ? {} : { moduleGraphs: [{
		id: "cn.vastplan.foundation.frontend.runtime.engine.react", version: "1.1.0", channel: "stable",
		target: "server", entry: "frontend/dist/server.js", digest: "c".repeat(64), packageSha256: "d".repeat(64), externals: ["stream"],
		nodes: [{ path: "frontend/dist/server.js", url: `server-object:${serverDigest}`, sha256: serverDigest,
			size: serverContent?.byteLength ?? 0, mediaType: "text/javascript", purpose: "entry", dependencies: [] }],
	}] };
  const snapshot = JSON.stringify({ specSha256: portalSpecDigest(spec), runtime, server });
  await mkdir(join(fixture.origin, "objects", digest.slice(0, 2)), { recursive: true });
  await mkdir(join(fixture.origin, "snapshots", createHash("sha256").update(`${spec.tenantId}\0${spec.id}`).digest("hex")), { recursive: true });
  await writeFile(objectPath(fixture.origin, digest), content);
  await writeFile(objectPath(fixture.origin, digest, true), gzipSync(content));
	if (serverContent !== undefined && serverDigest !== undefined) {
		await mkdir(join(fixture.origin, "objects", serverDigest.slice(0, 2)), { recursive: true });
		await writeFile(objectPath(fixture.origin, serverDigest), serverContent);
		await writeFile(objectPath(fixture.origin, serverDigest, true), gzipSync(serverContent));
	}
  await writeFile(snapshotPath(fixture.origin, spec.tenantId, spec.id, spec.revision), snapshot);
  return { spec, runtime, content, digest, ...(serverContent === undefined || serverDigest === undefined ? {} : { serverContent, serverDigest }) };
}
