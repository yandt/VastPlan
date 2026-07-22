import type { ActionSpec } from "@vastplan/ui-contract";
import type { CollectionPageDefinition } from "@vastplan/workbench-sdk";

export interface CollectionPageActionSnapshot {
  ready: boolean;
  selectedCount: number;
  visibleActionIDs: ReadonlySet<string>;
}

type ActionHandler = (action: ActionSpec) => void;
type Listener = () => void;

const emptySnapshot: CollectionPageActionSnapshot = Object.freeze({ ready: false, selectedCount: 0, visibleActionIDs: new Set<string>() });

class CollectionPageActionController {
  private snapshot = emptySnapshot;
  private handler: ActionHandler | undefined;
  private readonly listeners = new Set<Listener>();

  readonly subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  readonly getSnapshot = (): CollectionPageActionSnapshot => this.snapshot;

  bind(snapshot: Omit<CollectionPageActionSnapshot, "ready">, handler: ActionHandler): () => void {
    this.handler = handler;
    this.publish({ ...snapshot, ready: true });
    return () => {
      if (this.handler !== handler) return;
      this.handler = undefined;
      this.publish(emptySnapshot);
    };
  }

  run(action: ActionSpec): void {
    this.handler?.(action);
  }

  private publish(snapshot: CollectionPageActionSnapshot): void {
    this.snapshot = snapshot;
    for (const listener of this.listeners) listener();
  }
}

const controllers = new WeakMap<CollectionPageDefinition, CollectionPageActionController>();

export function collectionPageActionController(page: CollectionPageDefinition): CollectionPageActionController {
  const existing = controllers.get(page);
  if (existing !== undefined) return existing;
  const controller = new CollectionPageActionController();
  controllers.set(page, controller);
  return controller;
}
