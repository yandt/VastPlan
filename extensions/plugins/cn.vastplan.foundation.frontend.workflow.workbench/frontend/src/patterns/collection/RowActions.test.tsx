import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";

vi.mock("@vastplan/ui-primitives", () => ({
  message: (_namespace: string, _key: string, fallback: string) => fallback,
  usePortalI18n: () => ({ text: (value: unknown) => typeof value === "string" ? value : String((value as { fallback?: string }).fallback ?? "") }),
  usePortalUI: () => ({
    Stack: ({ justify, gap, children }: { justify?: string; gap?: string; children: ReactNode }) => <div data-justify={justify} data-gap={gap}>{children}</div>,
    IconButton: ({ icon, label, size, tone }: { icon: string; label: string; size?: string; tone?: string }) => <button type="button" data-icon={icon} data-size={size} data-tone={tone} aria-label={label} />,
    Popover: ({ children, trigger }: { children: ReactNode; trigger: (props: { ref: undefined; "aria-expanded": false; "aria-controls": string; onClick(): void; onKeyDown(): void }) => ReactNode }) => <div data-popover>{trigger({ ref: undefined, "aria-expanded": false, "aria-controls": "row-actions", onClick: () => undefined, onKeyDown: () => undefined })}{children}</div>,
  }),
}));

import { RowActions } from "./RowActions.js";

describe("RowActions", () => {
  it("centers data-defined semantic icon buttons with accessible tooltip labels", () => {
    const actions: ActionSpec[] = [
      { id: "edit", label: "编辑", icon: "edit", placement: "record.row", tone: "primary" },
      { id: "delete", label: "删除", icon: "remove", placement: "record.row", tone: "danger" },
      { id: "download", label: "下载", icon: "download", placement: "record.row", tone: "primary" },
    ];
    const html = renderToStaticMarkup(<RowActions actions={actions} row={{ id: "one" }} onRunAction={() => undefined} />);
    expect(html).toContain('data-justify="center"');
    expect(html).toContain('data-gap="sm"');
    expect(html).toContain('data-icon="edit"');
    expect(html).toContain('data-size="compact"');
    expect(html).toContain('data-tone="normal"');
    expect(html).toContain('aria-label="编辑"');
    expect(html).toContain('data-icon="remove"');
    expect(html).toContain('aria-label="删除"');
    expect(html).toContain("data-popover");
    expect(html).toContain('data-icon="download"');
    expect(html).toContain('aria-label="下载"');
    expect(html).toContain("display:inline-flex");
    expect(html).toContain("line-height:0");
  });
});
