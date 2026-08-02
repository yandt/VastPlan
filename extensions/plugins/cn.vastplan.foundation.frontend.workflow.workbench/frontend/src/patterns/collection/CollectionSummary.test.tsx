import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { CollectionSummary } from "./CollectionSummary.js";

vi.mock("@vastplan/ui-primitives", () => ({
  ComponentSizeProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  componentSizeRecipes: {
    control: { xs: { height: 24 }, sm: { height: 28 }, md: { height: 32 }, lg: { height: 40 } },
    layout: { xs: { gap: 4, sectionGap: 8 }, sm: { gap: 8, sectionGap: 12 }, md: { gap: 16, sectionGap: 24 }, lg: { gap: 24, sectionGap: 32 } },
    descriptions: { xs: { fontSize: 12 }, sm: { fontSize: 13 }, md: { fontSize: 14 }, lg: { fontSize: 16 } },
  },
  useComponentSize: (size?: string) => size ?? "md",
  usePortalI18n: () => ({ text: (value: string | { fallback?: string }) => typeof value === "string" ? value : value.fallback ?? "" }),
  usePortalUI: () => ({
    theme: { tokens: { color: { mutedText: "#666", text: "#111" } } },
    Descriptions: ({ title, items }: { title?: ReactNode; items: Array<{ id: string; label: ReactNode; value: ReactNode }> }) => <section data-descriptions>{title}<dl>{items.map((item) => <div key={item.id}><dt>{item.label}</dt><dd>{item.value}</dd></div>)}</dl></section>,
    Panel: ({ title, children }: { title?: string; children: ReactNode }) => <article data-panel>{title}{children}</article>,
    Status: ({ children }: { children: ReactNode }) => <strong>{children}</strong>,
  }),
}));

describe("CollectionSummary", () => {
  it("renders a content-width plain summary strip without bordered descriptions", () => {
    const markup = renderToStaticMarkup(<CollectionSummary summary={{ title: "Portal 状态", appearance: "plain", size: "sm", columns: 2, metrics: [{ id: "online", label: "已上线", value: 2, span: 2 }] }} />);
    expect(markup).toContain("Portal 状态");
    expect(markup).toContain("已上线");
    expect(markup).toContain('data-collection-summary="strip"');
    expect(markup).toContain('data-size="sm"');
    expect(markup).not.toContain("data-descriptions");
    expect(markup).not.toContain("data-panel");
  });

  it("keeps panel as the compatibility default", () => {
    const markup = renderToStaticMarkup(<CollectionSummary summary={{ title: "摘要", metrics: [{ id: "count", label: "数量", value: 1 }] }} />);
    expect(markup).toContain("data-panel");
  });
});
