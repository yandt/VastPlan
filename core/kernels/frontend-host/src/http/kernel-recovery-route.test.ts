import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it } from "vitest";
import { KernelRecoveryClient, serveKernelRecovery, serveKernelRecoveryPage } from "./kernel-recovery-route";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("Kernel Recovery public route", () => {
  it("returns only the bounded public stage projection without authentication", async () => {
    const upstream = {
      schemaVersion: 1, capsuleId: "private", repositoryId: "private", generation: 7, artifactCount: 3,
      overall: "RecoveryReady", scope: "cluster", clusterAvailable: true, nodes: 2, updatedAt: new Date().toISOString(),
      stages: [
        { id: "recovery", status: "Ready", ready: 2, required: 2, issues: ["private-unit"] },
        { id: "control-plane", status: "Pending", ready: 1, required: 2 },
        { id: "platform", status: "Pending", ready: 0, required: 2 },
      ],
    };
    const client = new KernelRecoveryClient("http://127.0.0.1:19090", async () => new Response(JSON.stringify(upstream), { status: 200, headers: { "content-type": "application/json" } }));
    const server = createServer((request, response) => { void serveKernelRecovery(client, request, response); });
    servers.push(server);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
    const response = await fetch(origin);
    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toBe("no-store");
    const body = await response.json() as Record<string, unknown>;
    expect(body).toEqual({ schemaVersion: 1, overall: "RecoveryReady", scope: "cluster", clusterAvailable: true, nodes: 2, updatedAt: upstream.updatedAt, stages: upstream.stages.map(({ issues: _issues, ...stage }) => stage) });
    expect(body).not.toHaveProperty("capsuleId");
    expect(body).not.toHaveProperty("repositoryId");
  });

  it("fails closed on malformed upstream state", async () => {
    const client = new KernelRecoveryClient("http://127.0.0.1:19090", async () => new Response(JSON.stringify({ overall: "Ready", stages: [] }), { status: 200 }));
    const server = createServer((request, response) => { void serveKernelRecovery(client, request, response); });
    servers.push(server);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    expect((await fetch(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)).status).toBe(503);
  });

  it("serves a plugin-independent recovery page", async () => {
    const upstream = { schemaVersion: 1, overall: "RecoveryReady", scope: "local", clusterAvailable: false, nodes: 1, updatedAt: new Date().toISOString(), stages: [
      { id: "recovery", status: "Ready", ready: 1, required: 1 },
      { id: "control-plane", status: "Pending", ready: 1, required: 2 },
      { id: "platform", status: "Pending", ready: 1, required: 3 },
    ] };
    const client = new KernelRecoveryClient("http://127.0.0.1:19090", async () => new Response(JSON.stringify(upstream), { status: 200 }));
    const server = createServer((request, response) => { void serveKernelRecoveryPage(client, request, response); });
    servers.push(server);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const response = await fetch(`http://127.0.0.1:${(server.address() as AddressInfo).port}`);
    const html = await response.text();
    expect(response.status).toBe(200);
    expect(response.headers.get("content-security-policy")).toContain("default-src 'none'");
    expect(html).toContain("VASTPLAN SAFE MODE");
    expect(html).toContain("恢复基础");
    expect(html).not.toContain("script");
  });
});
