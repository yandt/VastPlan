import type { PageRefreshSignal } from "@vastplan/ui-primitives";

export interface PageRefreshController extends PageRefreshSignal {
  invalidate(): void;
}

export function createPageRefreshController(): PageRefreshController {
  let revision = 0;
  const listeners = new Set<() => void>();
  return Object.freeze({
    subscribe(listener: () => void) { listeners.add(listener); return () => listeners.delete(listener); },
    getSnapshot() { return revision; },
    invalidate() { revision += 1; for (const listener of listeners) listener(); },
  });
}
