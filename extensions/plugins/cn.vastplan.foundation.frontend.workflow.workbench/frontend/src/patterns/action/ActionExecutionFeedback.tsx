import { useCallback, useEffect, useRef, useState } from "react";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";
const blockingFeedbackDelay = 650;

export interface WorkbenchActionExecution {
  id: string;
  label: string;
}

export interface ActionExecutionResult<T> {
  completed: boolean;
  value?: T;
}

/**
 * Owns one Workbench page's mutation lifecycle. A page may have only one
 * running action, preventing duplicate submissions while leaving the action
 * definition and business workflow unchanged.
 */
export function useActionExecution() {
  const [active, setActive] = useState<WorkbenchActionExecution>();
  const controllerRef = useRef<AbortController>();
  const activeRef = useRef(false);
  useEffect(() => () => { activeRef.current = false; controllerRef.current?.abort(); }, []);

  const run = useCallback(async <T,>(action: WorkbenchActionExecution, work: (signal: AbortSignal) => Promise<T>): Promise<ActionExecutionResult<T>> => {
    if (activeRef.current) return { completed: false };
    activeRef.current = true;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setActive(action);
    try {
      const result = await work(controller.signal);
      return controller.signal.aborted ? { completed: false } : { completed: true, value: result };
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = undefined;
        activeRef.current = false;
        setActive(undefined);
      }
    }
  }, []);

  return { active, running: active !== undefined, run, feedback: active === undefined ? null : <ActionExecutionFeedback active actionLabel={active.label} /> };
}

/** Displays immediate non-blocking feedback, then a governed wait dialog for slow actions. */
export function ActionExecutionFeedback({ active, actionLabel }: { active: boolean; actionLabel: string }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const delayed = useDelayedVisibility(active);
  if (!active) return null;
  const progress = i18n.text(message(namespace, "action.running", "正在{action}…", { action: actionLabel }));
  const description = i18n.text(message(namespace, "action.runningDescription", "操作正在执行，请勿关闭页面。"));
  return <>
    <div data-vastplan-action-progress role="status" aria-live="polite" style={{ position: "fixed", right: 20, bottom: 20, zIndex: 1000, width: "min(320px, calc(100vw - 40px))" }}>
      <ui.Panel size="sm"><ui.Stack direction="row" gap="sm" align="center"><ui.Busy size="xs" /><span>{progress}</span></ui.Stack></ui.Panel>
    </div>
    {delayed ? <ui.Dialog open title={i18n.text(message(namespace, "action.runningTitle", "正在处理"))} description={progress} size="sm" width="sm" contentOverflow="visible" onClose={() => undefined}>
      <ui.Stack gap="md" align="center"><ui.Busy size="md" label={description} /><span>{description}</span></ui.Stack>
    </ui.Dialog> : null}
  </>;
}

function useDelayedVisibility(active: boolean): boolean {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!active) { setVisible(false); return; }
    const timer = window.setTimeout(() => setVisible(true), blockingFeedbackDelay);
    return () => window.clearTimeout(timer);
  }, [active]);
  return visible;
}
