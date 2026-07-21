import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import type { InteractionPort } from "../capabilities/interaction-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("Interaction routes", () => {
  it("keeps browser identity and frontend surface server-owned", async () => {
    const calls: { operation: string; payload: unknown }[] = [];
    const interaction: InteractionPort = { async call(principal, operation, payload) {
      expect(principal).toMatchObject({ id: "alice", tenantId: "tenant-a" });
      calls.push({ operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown });
      return new TextEncoder().encode('{"state":"presented"}');
    } };
    const { origin, readHeaders, writeHeaders } = await startServer(interaction);
    expect((await fetch(`${origin}/v1/interactions`, { headers: readHeaders })).status).toBe(200);
    expect((await fetch(`${origin}/v1/interactions/interaction-1`, { headers: readHeaders })).status).toBe(200);
    expect((await fetch(`${origin}/v1/interactions/interaction-1/present`, { method: "POST", headers: writeHeaders, body: "{}" })).status).toBe(200);
    expect((await fetch(`${origin}/v1/interactions/interaction-1/respond`, { method: "POST", headers: writeHeaders, body: '{"interactionId":"interaction-1","decision":"answered","surface":"mobile"}' })).status).toBe(200);
    expect(calls).toEqual([
      { operation: "list", payload: { surface: "frontend" } },
      { operation: "get", payload: { id: "interaction-1" } },
      { operation: "present", payload: { id: "interaction-1", surface: "frontend" } },
      { operation: "respond", payload: { id: "interaction-1", surface: "frontend", response: { interactionId: "interaction-1", decision: "answered", surface: "mobile" } } },
    ]);
  });

  it("rejects writes without CSRF and encoded path separators", async () => {
    let calls = 0;
    const interaction: InteractionPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const { origin, readHeaders } = await startServer(interaction);
    const missingCSRF = await fetch(`${origin}/v1/interactions/interaction-1/present`, { method: "POST", headers: { ...readHeaders, "Content-Type": "application/json" }, body: "{}" });
    expect(missingCSRF.status).toBe(403);
    const separator = await fetch(`${origin}/v1/interactions/interaction%2Fadmin`, { headers: readHeaders });
    expect(separator.status).toBe(404);
    expect(calls).toBe(0);
  });
});

async function startServer(interaction: InteractionPort): Promise<{ origin: string; readHeaders: Record<string, string>; writeHeaders: Record<string, string> }> {
  const root = await createPortalFixture();
  const sessionFile = join(root, "sessions.json");
  await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000));
  const assets = await PortalAssets.load(root);
  const identity = await FileIdentityProvider.open(sessionFile);
  const server = createServer(createPortalHandler({ assets, identity, interaction, secureCookies: false }));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const sessionCookie = "vastplan_session=browser-token";
  const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
  const token = (await csrf.json() as { token: string }).token;
  return {
    origin,
    readHeaders: { Cookie: sessionCookie },
    writeHeaders: { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" },
  };
}
