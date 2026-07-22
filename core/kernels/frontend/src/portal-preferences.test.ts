import { beforeEach, describe, expect, it, vi } from "vitest";
import { PortalPreferenceConflict, PortalPreferenceSession } from "./portal-preferences";

const portal = {
  revision: 1, id: "operations", tenantId: "tenant-a", route: "/operations",
  renderAdapter: { id: "cn.vastplan.render", uiContract: "^4.0.0", config: { defaultRenderer: "arco", allowedRenderers: ["arco", "mui"], userSelectable: true } },
  shell: { id: "cn.vastplan.shell", uiContract: "^4.0.0", config: { defaultTemplate: "standard", allowedTemplates: ["standard", "top-navigation"], userSelectable: true } },
  workbench: { id: "cn.vastplan.workbench", uiContract: "^4.0.0" },
} as any;

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) });
});

describe("PortalPreferenceSession", () => {
  it("uses the remote preference before local cache and filters revoked choices", async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ revision: 3, scope: scope(), values: { rendererId: "mui", shellTemplateId: "revoked" } }), { status: 200 }));
    const session = await PortalPreferenceSession.open(fetcher, "/operations", portal);
    expect(session.resolve(portal)).toEqual({ rendererID: "mui" });
  });

  it("keeps renderer changes pending until the server CAS succeeds", async () => {
    let conflict = false;
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/v1/csrf") return new Response(JSON.stringify({ token: "a".repeat(64) }), { status: 200 });
      if (init?.method === "PUT") return new Response(conflict ? JSON.stringify({ error: "portal_preference_conflict" }) : JSON.stringify({ revision: 1, scope: scope(), values: { rendererId: "mui" } }), { status: conflict ? 409 : 200 });
      return new Response(JSON.stringify({ revision: 0, scope: scope(), values: {} }), { status: 200 });
    });
    const session = await PortalPreferenceSession.open(fetcher, "/operations", portal);
    session.stageRenderer("mui", portal);
    expect(session.resolve(portal).rendererID).toBe("mui");
    conflict = true;
    await expect(session.commitPendingRenderer(portal)).rejects.toBeInstanceOf(PortalPreferenceConflict);
    session.discardPendingRenderer(portal);
    expect(session.resolve(portal).rendererID).toBeUndefined();
  });

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
});

function scope() {
  return { portalId: "operations", renderer: { id: "cn.vastplan.render", contractMajor: 4 }, shell: { id: "cn.vastplan.shell", contractMajor: 4 }, workbench: { id: "cn.vastplan.workbench", contractMajor: 4 } };
}
