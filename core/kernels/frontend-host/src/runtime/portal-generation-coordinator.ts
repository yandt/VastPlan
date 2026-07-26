import { randomBytes } from "node:crypto";
import type { FrontendServerRenderInput } from "@vastplan/frontend-engine-contract";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import type { PreparedServerGeneration } from "../workers/server-generation-manager";
import { PortalActivationCatalog, type PortalActivation } from "./portal-activation-catalog";
import { portalSpecDigest } from "./portal-runtime-contract";

const defaultTransactionTTL = 120_000;

export type PortalGenerationCoordination =
  | { readonly state: "prepared"; readonly activationId: number; readonly transactionId: string }
  | { readonly state: "committed"; readonly activationId: number };

export interface ServerGenerationLifecycle {
  prepare(tenantId: string, spec: PortalActivation["resolved"], input: FrontendServerRenderInput): Promise<PreparedServerGeneration | undefined>;
  isCommitted(prepared: PreparedServerGeneration): boolean;
  commit(prepared: PreparedServerGeneration): void;
  discard(prepared: PreparedServerGeneration): Promise<void>;
}

export interface PortalGenerationCoordinationPort {
  prepare(principal: Principal, active: PortalActivation, input: FrontendServerRenderInput): Promise<PortalGenerationCoordination | undefined>;
  commit(principal: Principal, transactionID: string, signal?: AbortSignal): Promise<PortalGenerationCoordination>;
}

interface Transaction {
  readonly id: string;
  readonly tenantId: string;
  readonly portalId: string;
  readonly activationId: number;
  readonly portalSpecDigest: string;
  readonly prepared: PreparedServerGeneration;
  state: "prepared" | "committed";
  timer: NodeJS.Timeout;
}

export class PortalGenerationCoordinationError extends Error {
  public constructor(public readonly code: "transaction_not_found" | "activation_changed" | "portal_audience_forbidden") {
    super(code);
    this.name = "PortalGenerationCoordinationError";
  }
}

/** Linearizes Browser/Server generation commit at one authenticated Node endpoint. */
export class PortalGenerationCoordinator {
  private readonly activations: PortalActivationCatalog;
  private readonly transactions = new Map<string, Transaction>();
  private readonly bySlot = new Map<string, Transaction>();

  public constructor(composer: PortalComposerPort, private readonly generations: ServerGenerationLifecycle, private readonly ttlMilliseconds = defaultTransactionTTL) {
    this.activations = new PortalActivationCatalog(composer);
  }

  public async prepare(principal: Principal, active: PortalActivation, input: FrontendServerRenderInput): Promise<PortalGenerationCoordination | undefined> {
    const prepared = await this.generations.prepare(principal.tenantId, active.resolved, input);
    if (prepared === undefined) return undefined;
    if (this.generations.isCommitted(prepared)) return Object.freeze({ state: "committed", activationId: active.id });
    const existing = this.bySlot.get(prepared.slot);
    if (existing?.prepared.key === prepared.key && existing.activationId === active.id && existing.state === "prepared") return projection(existing);
    if (existing !== undefined && existing.activationId > active.id) {
      await this.generations.discard(prepared);
      throw new PortalGenerationCoordinationError("activation_changed");
    }
    if (existing !== undefined) await this.expire(existing);
    const transaction = this.createTransaction(principal.tenantId, active, prepared);
    this.transactions.set(transaction.id, transaction);
    this.bySlot.set(prepared.slot, transaction);
    return projection(transaction);
  }

  public async commit(principal: Principal, transactionID: string, signal?: AbortSignal): Promise<PortalGenerationCoordination> {
    const transaction = this.transactions.get(transactionID);
    if (transaction === undefined || transaction.tenantId !== principal.tenantId) throw new PortalGenerationCoordinationError("transaction_not_found");
    if (transaction.state === "committed") return Object.freeze({ state: "committed", activationId: transaction.activationId });
    const activations = await this.activations.list(principal, signal);
    const active = activations.find((candidate) => candidate.status === "Current" && candidate.tenantId === transaction.tenantId
      && candidate.portalId === transaction.portalId && candidate.id === transaction.activationId && candidate.resolved.revision === transaction.activationId
      && portalSpecDigest(candidate.resolved) === transaction.portalSpecDigest);
    if (active === undefined) {
      await this.expire(transaction);
      throw new PortalGenerationCoordinationError("activation_changed");
    }
    if (!this.activations.audienceAllows(active, principal)) throw new PortalGenerationCoordinationError("portal_audience_forbidden");
    this.generations.commit(transaction.prepared);
    transaction.state = "committed";
    clearTimeout(transaction.timer);
    transaction.timer = this.expiryTimer(transaction, this.ttlMilliseconds);
    return Object.freeze({ state: "committed", activationId: transaction.activationId });
  }

  public async shutdown(): Promise<void> {
    const pending = [...this.transactions.values()];
    this.transactions.clear();
    this.bySlot.clear();
    for (const transaction of pending) {
      clearTimeout(transaction.timer);
      if (transaction.state === "prepared") await this.generations.discard(transaction.prepared);
    }
  }

  private createTransaction(tenantId: string, active: PortalActivation, prepared: PreparedServerGeneration): Transaction {
    const transaction = {
      id: randomBytes(32).toString("hex"), tenantId, portalId: active.portalId, activationId: active.id,
      portalSpecDigest: portalSpecDigest(active.resolved), prepared, state: "prepared" as const,
    } as Transaction;
    transaction.timer = this.expiryTimer(transaction, this.ttlMilliseconds);
    return transaction;
  }

  private expiryTimer(transaction: Transaction, delay: number): NodeJS.Timeout {
    const timer = setTimeout(() => void this.expire(transaction).catch((error: unknown) => {
      process.stderr.write(`${JSON.stringify({ level: "error", message: "portal generation expiry failed", error: error instanceof Error ? error.message : String(error) })}\n`);
    }), delay);
    timer.unref();
    return timer;
  }

  private async expire(transaction: Transaction): Promise<void> {
    if (this.transactions.get(transaction.id) !== transaction) return;
    this.transactions.delete(transaction.id);
    if (this.bySlot.get(transaction.prepared.slot) === transaction) this.bySlot.delete(transaction.prepared.slot);
    clearTimeout(transaction.timer);
    if (transaction.state === "prepared") await this.generations.discard(transaction.prepared);
  }
}

function projection(transaction: Transaction): PortalGenerationCoordination {
  return transaction.state === "prepared"
    ? Object.freeze({ state: "prepared", activationId: transaction.activationId, transactionId: transaction.id })
    : Object.freeze({ state: "committed", activationId: transaction.activationId });
}
