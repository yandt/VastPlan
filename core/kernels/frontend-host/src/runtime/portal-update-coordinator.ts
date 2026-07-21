import type { Principal } from "../identity/identity-provider";
import { PortalActivationCatalog, type PortalActivation } from "./portal-activation-catalog";
import type { PortalDeliveryStore } from "./portal-delivery-store";
import type { PortalSpec } from "./portal-runtime-contract";

export interface PortalUpdate {
  readonly portalId: string;
  readonly activationId: number;
  readonly mode: "generation" | "host-epoch";
}

interface Monitor {
  principal: Principal;
  active: PortalActivation;
  listeners: Set<(update: PortalUpdate) => void>;
  timer: NodeJS.Timeout;
  syncing: boolean;
}

export class PortalUpdateCoordinator {
  private readonly monitors = new Map<string, Monitor>();

  public constructor(
    private readonly activations: PortalActivationCatalog,
    private readonly delivery: PortalDeliveryStore,
    private readonly intervalMilliseconds = 5_000,
  ) {}

  public subscribe(principal: Principal, active: PortalActivation, listener: (update: PortalUpdate) => void): () => void {
    const key = `${principal.tenantId}\0${active.portalId}`;
    let monitor = this.monitors.get(key);
    if (monitor === undefined) {
      const timer = setInterval(() => void this.sync(key), this.intervalMilliseconds);
      timer.unref();
      monitor = { principal, active, listeners: new Set(), timer, syncing: false };
      this.monitors.set(key, monitor);
      void this.sync(key);
    }
    if (active.id > monitor.active.id) monitor.active = active;
    monitor.listeners.add(listener);
    return () => {
      const current = this.monitors.get(key);
      if (current === undefined) return;
      current.listeners.delete(listener);
      if (current.listeners.size === 0) {
        clearInterval(current.timer);
        this.monitors.delete(key);
      }
    };
  }

  private async sync(key: string): Promise<void> {
    const monitor = this.monitors.get(key);
    if (monitor === undefined || monitor.syncing) return;
    monitor.syncing = true;
    try {
      const activations = await this.activations.list(monitor.principal);
      const current = activations.find((activation) => activation.status === "Current"
        && activation.tenantId === monitor.principal.tenantId && activation.portalId === monitor.active.portalId
        && activation.id === activation.resolved.revision);
      if (current === undefined || current.id <= monitor.active.id) return;
      await this.delivery.runtime(current.tenantId, current.resolved);
      const update = { portalId: current.portalId, activationId: current.id, mode: classifyPortalUpdate(monitor.active.resolved, current.resolved) } as const;
      monitor.active = current;
      for (const listener of monitor.listeners) listener(update);
    } catch {
      // Durable Activation remains the truth. A later poll retries; no unsafe
      // update is announced before its immutable delivery snapshot is ready.
    } finally {
      const current = this.monitors.get(key);
      if (current !== undefined) current.syncing = false;
    }
  }
}

export function classifyPortalUpdate(previous: PortalSpec, current: PortalSpec): "generation" | "host-epoch" {
  const boundaryPaths = [
    ["renderAdapter", "id"], ["renderAdapter", "version"], ["renderAdapter", "channel"],
    ["renderAdapter", "uiContract"], ["renderAdapter", "config", "defaultRenderer"],
    ["shell", "uiContract"], ["workbench", "uiContract"],
  ];
  return boundaryPaths.some((path) => nested(previous, path) !== nested(current, path)) ? "host-epoch" : "generation";
}

function nested(value: unknown, path: readonly string[]): unknown {
  let current = value;
  for (const part of path) {
    if (typeof current !== "object" || current === null || Array.isArray(current)) return undefined;
    current = (current as Readonly<Record<string, unknown>>)[part];
  }
  return current;
}
