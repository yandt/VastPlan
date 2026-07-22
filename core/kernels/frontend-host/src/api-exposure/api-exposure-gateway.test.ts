import { createServer } from "node:http";
import { afterEach, describe, expect, it } from "vitest";
import { CapabilityApplicationError, type TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import type { IdentityProvider, Principal } from "../identity/identity-provider";
import { APIExposureGateway } from "./api-exposure-gateway";
import type { APIExposureCatalogPort } from "./api-exposure-contract";
import { exampleCatalog } from "./api-exposure-test-fixture";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => { await Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))); });

describe("APIExposureGateway", () => {
  it("routes through opaque key, projects bounded invocation, and never exposes plugin identity", async () => {
    const calls: unknown[] = [];
    const origin = await startGateway(principal(), {
      async invoke(_principal, route, operation, payload) {
        calls.push({ route, operation, payload: JSON.parse(new TextDecoder().decode(payload)) });
        return Buffer.from('{"ok":true}');
      },
    });
    const response = await fetch(`${origin}/api/r/aaaaaaaaaaaaaaaaaaaa/v1/items/42?tag=a&tag=b`);
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ ok: true });
    expect(calls).toEqual([{
      route: { capability: "platform.demo", logicalService: "backend.default", routingDomain: "platform.default" }, operation: "listItems",
      payload: { schemaVersion: "v1", routeId: "platform.demo.list", method: "GET", pathParams: { itemId: "42" }, query: { tag: ["a", "b"] }, body: {} },
    }]);
    expect(response.url).not.toContain("cn.vastplan");
  });

  it("fails closed on tenant, authentication profile, permission and invalid upstream response", async () => {
    for (const candidate of [
      { ...principal(), tenantId: "tenant-b" },
      { ...principal(), authenticationProfileId: "auth.other" },
      { ...principal(), roles: [] },
    ]) {
      const origin = await startGateway(candidate, successfulInvoker());
      expect((await fetch(`${origin}/api/r/aaaaaaaaaaaaaaaaaaaa/v1/items/42`)).status).toBe(403);
    }
    const origin = await startGateway(principal(), { async invoke() { return Buffer.from('{"unexpected":true}'); } });
    const response = await fetch(`${origin}/api/r/aaaaaaaaaaaaaaaaaaaa/v1/items/42`);
    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ error: "upstream_invalid_response" });
  });

  it("maps only declared application errors", async () => {
    const declared = await startGateway(principal(), { async invoke() { throw new CapabilityApplicationError("platform.demo.not_found", "internal detail"); } });
    const declaredResponse = await fetch(`${declared}/api/r/aaaaaaaaaaaaaaaaaaaa/v1/items/42`);
    expect(declaredResponse.status).toBe(404);
    expect(await declaredResponse.text()).not.toContain("internal detail");

    const undeclared = await startGateway(principal(), { async invoke() { throw new CapabilityApplicationError("internal.secret", "secret"); } });
    const undeclaredResponse = await fetch(`${undeclared}/api/r/aaaaaaaaaaaaaaaaaaaa/v1/items/42`);
    expect(undeclaredResponse.status).toBe(502);
    expect(await undeclaredResponse.json()).toEqual({ error: "upstream_rejected" });
  });
});

function principal(): Principal {
  return { id: "alice", tenantId: "tenant-a", portalId: "operations", roles: ["platform.demo.read"], authenticationProfileId: "auth.file" };
}

function successfulInvoker(): TrustedCapabilityInvoker {
  return { async invoke() { return Buffer.from('{"ok":true}'); } };
}

async function startGateway(principalValue: Principal, invoker: TrustedCapabilityInvoker): Promise<string> {
  const resolved = exampleCatalog().exposures[0];
  const catalog: APIExposureCatalogPort = { async resolve() { return resolved; } };
  const identity: IdentityProvider = { async authenticate() { return principalValue; } };
  const gateway = new APIExposureGateway(catalog, identity, invoker, false);
  const server = createServer((request, response) => void gateway.handle(request, response, new URL(request.url ?? "/", "http://local").pathname));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (address === null || typeof address === "string") throw new Error("测试服务器地址无效");
  return `http://127.0.0.1:${address.port}`;
}
