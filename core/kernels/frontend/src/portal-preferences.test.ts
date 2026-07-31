import { beforeEach, describe, expect, it, vi } from "vitest";
import { PortalPreferenceSession } from "./portal-preferences";

const portal = {
  revision: 1, id: "operations", tenantId: "tenant-a", route: "/operations",
  renderAdapter: { id: "cn.vastplan.render", uiContract: "^5.0.0", config: { defaultRenderer: "primary", allowedRenderers: ["primary", "alternate"], userSelectable: true } },
  shell: { id: "cn.vastplan.shell", uiContract: "^5.0.0", config: { defaultTemplate: "standard", allowedTemplates: ["standard", "top-navigation"], userSelectable: true } },
  workbench: { id: "cn.vastplan.workbench", uiContract: "^5.0.0" },
} as any;

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) });
});

describe("PortalPreferenceSession", () => {
  it("serializes collection writes and merges again after a concurrent CAS update", async () => {
    let getRevision = 1;
    const puts: Array<Record<string, any>> = [];
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/v1/csrf") return new Response(JSON.stringify({ token: "b".repeat(64) }), { status: 200 });
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as Record<string, any>;
        puts.push(body);
        if (puts.length === 1) return new Response(JSON.stringify({ error: "portal_preference_conflict" }), { status: 409 });
        return new Response(JSON.stringify({ revision: 3, scope: scope(), values: body.values }), { status: 200 });
      }
      return new Response(JSON.stringify({ revision: getRevision++, scope: scope(), values: { collections: { audit: { pageSize: 50 } } } }), { status: 200 });
    });
    const session = await PortalPreferenceSession.open(fetcher, "/operations", portal);
    await session.writeCollection("services", { columns: ["name", "id"], hiddenColumns: ["id"], density: "compact", pageSize: 20 });
    expect(puts).toHaveLength(2);
    expect(puts[1]?.expectedRevision).toBe(2);
    expect(puts[1]?.values.collections).toEqual({ audit: { pageSize: 50 }, services: { columns: ["name", "id"], hiddenColumns: ["id"], density: "compact", pageSize: 20 } });
    expect(session.readCollection("audit")).toEqual({ pageSize: 50 });
  });

  it("uses the server-authored preference scope when HMR overlays newer UI contracts", async () => {
    const storedScope = {
      portalId: "operations",
      renderer: { id: "cn.vastplan.render", contractMajor: 4 },
      shell: { id: "cn.vastplan.shell", contractMajor: 4 },
      workbench: { id: "cn.vastplan.workbench", contractMajor: 4 },
    };
    const hmrPortal = { ...portal, preferenceScope: storedScope };
    let put: Record<string, any> | undefined;
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/v1/csrf") return new Response(JSON.stringify({ token: "c".repeat(64) }), { status: 200 });
      if (init?.method === "PUT") {
        put = JSON.parse(String(init.body)) as Record<string, any>;
        return new Response(JSON.stringify({ revision: 29, scope: storedScope, values: put.values }), { status: 200 });
      }
      return new Response(JSON.stringify({ revision: 28, scope: storedScope, values: { collections: {} } }), { status: 200 });
    });
    const session = await PortalPreferenceSession.open(fetcher, "/operations/settings/portals/profiles", hmrPortal);
    await session.writeCollection("portal-profiles", { columns: ["id", "status"], hiddenColumns: ["status"] });
    expect(put?.expectedRevision).toBe(28);
    expect(session.readCollection("portal-profiles")).toEqual({ columns: ["id", "status"], hiddenColumns: ["status"] });
  });
});

function scope() {
  return { portalId: "operations", workbench: { id: "cn.vastplan.workbench", contractMajor: 5 } };
}
