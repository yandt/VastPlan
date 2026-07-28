import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { ActionSpec } from "@vastplan/ui-contract";

vi.mock("@vastplan/ui-primitives", () => ({
  message: (_namespace: string, _key: string, fallback: string) => fallback,
  usePortalI18n: () => ({ text: (value: unknown) => typeof value === "string" ? value : String((value as { fallback?: string }).fallback ?? "") }),
  usePortalUI: () => ({
    Stack: ({ justify, children }: { justify?: string; children: ReactNode }) => <div data-justify={justify}>{children}</div>,
    IconButton: ({ icon, label, size }: { icon: string; label: string; size?: string }) => <button type="button" data-icon={icon} data-size={size} aria-label={label} />,
    Popover: ({ children }: { children: ReactNode }) => <div data-popover>{children}</div>,
  }),
}));

import { RowActions } from "./RowActions.js";

describe("RowActions", () => {
  it("centers data-defined semantic icon buttons with accessible tooltip labels", () => {
    const actions: ActionSpec[] = [
      { id: "edit", label: "编辑", icon: "edit", placement: "record.row" },
      { id: "delete", label: "删除", icon: "remove", placement: "record.row", tone: "danger" },
      { id: "download", label: "下载", icon: "download", placement: "record.row" },
    ];
    const html = renderToStaticMarkup(<RowActions actions={actions} row={{ id: "one" }} onRunAction={() => undefined} />);
    expect(html).toContain('data-justify="center"');
    expect(html).toContain('data-icon="edit"');
    expect(html).toContain('data-size="compact"');
    expect(html).toContain('aria-label="编辑"');
    expect(html).toContain('data-icon="remove"');
    expect(html).toContain('aria-label="删除"');
    expect(html).toContain("data-popover");
    expect(html).toContain('data-icon="download"');
    expect(html).toContain('aria-label="下载"');
  });
});
