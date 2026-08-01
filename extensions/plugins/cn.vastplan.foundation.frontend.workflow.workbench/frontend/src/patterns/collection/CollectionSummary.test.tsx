import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { CollectionSummary } from "./CollectionSummary.js";

vi.mock("@vastplan/ui-primitives", () => ({
  usePortalI18n: () => ({ text: (value: string | { fallback?: string }) => typeof value === "string" ? value : value.fallback ?? "" }),
  usePortalUI: () => ({
    Descriptions: ({ title, items }: { title?: ReactNode; items: Array<{ id: string; label: ReactNode; value: ReactNode }> }) => <section data-descriptions>{title}<dl>{items.map((item) => <div key={item.id}><dt>{item.label}</dt><dd>{item.value}</dd></div>)}</dl></section>,
    Panel: ({ title, children }: { title?: string; children: ReactNode }) => <article data-panel>{title}{children}</article>,
    Status: ({ children }: { children: ReactNode }) => <strong>{children}</strong>,
  }),
}));

describe("CollectionSummary", () => {
  it("renders a plain summary without the compatibility panel", () => {
    const markup = renderToStaticMarkup(<CollectionSummary summary={{ title: "Portal 状态", appearance: "plain", columns: 2, metrics: [{ id: "online", label: "已上线", value: 2, span: 2 }] }} />);
    expect(markup).toContain("Portal 状态");
    expect(markup).toContain("已上线");
    expect(markup).not.toContain("data-panel");
  });

  it("keeps panel as the compatibility default", () => {
    const markup = renderToStaticMarkup(<CollectionSummary summary={{ title: "摘要", metrics: [{ id: "count", label: "数量", value: 1 }] }} />);
    expect(markup).toContain("data-panel");
  });
});
