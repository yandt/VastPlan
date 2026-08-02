import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  message: (_namespace: string, _key: string, fallback: string, values?: Record<string, string>) => values === undefined
    ? fallback
    : fallback.replace("{action}", values.action ?? ""),
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({
    Panel: ({ children }: { children: unknown }) => <section>{children as never}</section>,
    Stack: ({ children }: { children: unknown }) => <div>{children as never}</div>,
    Busy: ({ label }: { label?: string }) => <span data-busy>{label}</span>,
    Dialog: ({ children }: { children: unknown }) => <aside>{children as never}</aside>,
  }),
}));

import { ActionExecutionFeedback } from "./ActionExecutionFeedback.js";

describe("ActionExecutionFeedback", () => {
  it("shows immediate accessible feedback while an action is running", () => {
    const html = renderToStaticMarkup(<ActionExecutionFeedback active actionLabel="发布" />);

    expect(html).toContain("data-vastplan-action-progress");
    expect(html).toContain("正在发布…");
    expect(html).toContain('role="status"');
  });
});
