import { describe, expect, it, vi } from "vitest";
import { completeAuthenticationNavigation, installAccessPageResumeGuard } from "./access-login-lifecycle";

describe("access login lifecycle", () => {
  it("replaces the completed login document instead of retaining a stale transaction in history", () => {
    const replace = vi.fn();
    completeAuthenticationNavigation({ replace }, "/operations");
    expect(replace).toHaveBeenCalledWith("/operations");
  });

  it("reloads a login document restored from the browser back-forward cache", () => {
    const target = new EventTarget();
    const reload = vi.fn();
    const dispose = installAccessPageResumeGuard(target, reload);

    target.dispatchEvent(pageShow(false));
    expect(reload).not.toHaveBeenCalled();
    target.dispatchEvent(pageShow(true));
    expect(reload).toHaveBeenCalledOnce();

    dispose();
    target.dispatchEvent(pageShow(true));
    expect(reload).toHaveBeenCalledOnce();
  });
});

function pageShow(persisted: boolean): Event {
  const event = new Event("pageshow");
  Object.defineProperty(event, "persisted", { value: persisted });
  return event;
}
