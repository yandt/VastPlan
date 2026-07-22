import type { ActionSpec } from "@vastplan/ui-contract";

export interface PageActionSnapshot {
  ready: boolean;
  selectedCount: number;
  visibleActionIDs: ReadonlySet<string>;
}

type ActionHandler = (action: ActionSpec) => void;
type Listener = () => void;

const emptySnapshot: PageActionSnapshot = Object.freeze({ ready: false, selectedCount: 0, visibleActionIDs: new Set<string>() });

class PageActionController {
  private snapshot = emptySnapshot;
  private handler: ActionHandler | undefined;
  private readonly listeners = new Set<Listener>();

  readonly subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  readonly getSnapshot = (): PageActionSnapshot => this.snapshot;

  bind(snapshot: Omit<PageActionSnapshot, "ready">, handler: ActionHandler): () => void {
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

  private publish(snapshot: PageActionSnapshot): void {
    this.snapshot = snapshot;
    for (const listener of this.listeners) listener();
  }
}

const controllers = new WeakMap<object, PageActionController>();

export function pageActionController(page: object): PageActionController {
  const existing = controllers.get(page);
  if (existing !== undefined) return existing;
  const controller = new PageActionController();
  controllers.set(page, controller);
  return controller;
}
