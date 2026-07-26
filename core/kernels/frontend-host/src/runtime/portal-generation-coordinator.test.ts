import { afterEach, describe, expect, it, vi } from "vitest";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { PortalGenerationCoordinator, type ServerGenerationLifecycle } from "./portal-generation-coordinator";
import type { PortalActivation } from "./portal-activation-catalog";

afterEach(() => vi.useRealTimers());

const principal: Principal = { id: "operator", tenantId: "tenant-a", roles: ["portal.read"] };
const active: PortalActivation = {
  id: 7, tenantId: "tenant-a", portalId: "operations", status: "Current",
  resolved: { revision: 7, id: "operations", tenantId: "tenant-a", route: "/operations", audience: ["portal.read"] },
};
const input = { generation: 7, tenantId: "tenant-a", portalId: "operations", path: "/operations", locale: "zh-CN", branding: {} };

describe("PortalGenerationCoordinator", () => {
  it("reuses one prepared transaction and commits it idempotently after Activation revalidation", async () => {
    const lifecycle = fakeLifecycle();
    const coordinator = new PortalGenerationCoordinator(composer(() => [active]), lifecycle);
    const first = await coordinator.prepare(principal, active, input);
    const second = await coordinator.prepare(principal, active, input);
    expect(first).toEqual(second);
    const transactionID = first?.state === "prepared" ? first.transactionId : "";
    expect(await coordinator.commit(principal, transactionID)).toEqual({ state: "committed", activationId: 7 });
    expect(await coordinator.commit(principal, transactionID)).toEqual({ state: "committed", activationId: 7 });
    expect(lifecycle.commit).toHaveBeenCalledOnce();
    await coordinator.shutdown();
  });

  it("discards a stale candidate instead of committing after the current Activation changes", async () => {
    const lifecycle = fakeLifecycle();
    let activations: readonly PortalActivation[] = [active];
    const coordinator = new PortalGenerationCoordinator(composer(() => activations), lifecycle);
    const prepared = await coordinator.prepare(principal, active, input);
    activations = [{ ...active, id: 8, resolved: { ...active.resolved, revision: 8 } }];
    const transactionID = prepared?.state === "prepared" ? prepared.transactionId : "";
    await expect(coordinator.commit(principal, transactionID)).rejects.toMatchObject({ code: "activation_changed" });
    expect(lifecycle.commit).not.toHaveBeenCalled();
    expect(lifecycle.discard).toHaveBeenCalledOnce();
  });

  it("rejects same-revision PortalSpec drift and never lets an older preparation overtake a newer slot", async () => {
    const driftLifecycle = fakeLifecycle();
    const drifted = { ...active, resolved: { ...active.resolved, route: "/changed" } };
    const driftCoordinator = new PortalGenerationCoordinator(composer(() => [drifted]), driftLifecycle);
    const first = await driftCoordinator.prepare(principal, active, input);
    await expect(driftCoordinator.commit(principal, first?.state === "prepared" ? first.transactionId : "")).rejects.toMatchObject({ code: "activation_changed" });
    expect(driftLifecycle.commit).not.toHaveBeenCalled();

    const discard = vi.fn(async () => undefined);
    const lifecycle: ServerGenerationLifecycle = {
      async prepare(_tenant, spec) { return { slot: "tenant-a/operations", key: `tenant-a/operations/${spec.revision}/${String(spec.revision).repeat(64)}` }; },
      isCommitted() { return false; }, commit() {}, discard,
    };
    const coordinator = new PortalGenerationCoordinator(composer(() => [active]), lifecycle);
    const newer = { ...active, id: 8, resolved: { ...active.resolved, revision: 8 } };
    await coordinator.prepare(principal, newer, { ...input, generation: 8 });
    await expect(coordinator.prepare(principal, active, input)).rejects.toMatchObject({ code: "activation_changed" });
    expect(discard).toHaveBeenCalledWith(expect.objectContaining({ key: expect.stringContaining("/7/") }));
    await coordinator.shutdown();
  });

  it("expires an abandoned prepared candidate", async () => {
    vi.useFakeTimers();
    const lifecycle = fakeLifecycle();
    const coordinator = new PortalGenerationCoordinator(composer(() => [active]), lifecycle, 100);
    await coordinator.prepare(principal, active, input);
    await vi.advanceTimersByTimeAsync(101);
    expect(lifecycle.discard).toHaveBeenCalledOnce();
    await coordinator.shutdown();
  });
});

function composer(values: () => readonly PortalActivation[]): PortalComposerPort {
  return { async call(_principal, operation) {
    if (operation !== "listActivations") throw new Error("unexpected operation");
    return new TextEncoder().encode(JSON.stringify(values()));
  } };
}

function fakeLifecycle(): ServerGenerationLifecycle & { commit: ReturnType<typeof vi.fn>; discard: ReturnType<typeof vi.fn> } {
  let committed = false;
  const prepared = { slot: "tenant-a/operations", key: "tenant-a/operations/7/" + "a".repeat(64) };
  return {
    async prepare() { return prepared; },
    isCommitted() { return committed; },
    commit: vi.fn(() => { committed = true; }),
    discard: vi.fn(async () => undefined),
  };
}
