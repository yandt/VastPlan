import { describe, expect, it, vi } from "vitest";
import { authenticationExpiryDelay, completeAuthenticationNavigation, installAccessPageResumeGuard } from "./access-login-lifecycle";

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

  it("turns the trusted step expiry into a bounded automatic restart delay", () => {
    const now = Date.parse("2026-08-04T12:00:00.000Z");
    expect(authenticationExpiryDelay("2026-08-04T12:00:10.000Z", now)).toBe(10_025);
    expect(authenticationExpiryDelay("2026-08-04T11:59:59.000Z", now)).toBe(0);
    expect(authenticationExpiryDelay("invalid", now)).toBeUndefined();
  });
});

function pageShow(persisted: boolean): Event {
  const event = new Event("pageshow");
  Object.defineProperty(event, "persisted", { value: persisted });
  return event;
}
