import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("antd", async (importOriginal) => {
  const actual = await importOriginal<typeof import("antd")>();
  return { ...actual, Modal: ({ title }: { title: unknown }) => <div data-modal-title>{title as never}</div> };
});

import { Dialog } from "./feedback.js";

describe("Ant Design Dialog description", () => {
  it("places contextual description beneath the title instead of in the Dialog body", () => {
    const html = renderToStaticMarkup(<Dialog open title="补充审批证据" description="请按当前审批策略补充证据" onClose={() => undefined}>表单字段</Dialog>);
    expect(html).toContain("vp-antd-dialog-title");
    expect(html).toContain("补充审批证据");
    expect(html).toContain("请按当前审批策略补充证据");
    expect(html).toContain("margin-top:4px");
  });
});
