import { afterEach, describe, expect, it, vi } from "vitest";
import { resolveAccessSystemScheme } from "./access-system-scheme";

afterEach(() => vi.unstubAllGlobals());

describe("Access system appearance", () => {
  it("uses light appearance when the browser does not expose a preference", () => {
    expect(resolveAccessSystemScheme()).toBe("light");
  });

  it("uses the browser's dark preference without reading user preferences", () => {
    vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true })));
    expect(resolveAccessSystemScheme()).toBe("dark");
  });
});
