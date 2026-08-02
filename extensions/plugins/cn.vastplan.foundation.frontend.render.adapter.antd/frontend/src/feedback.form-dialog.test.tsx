import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("antd", async (importOriginal) => {
  const actual = await importOriginal<typeof import("antd")>();
  return {
    ...actual,
    Modal: ({ className, styles, children }: { className?: string; styles?: { container?: Record<string, unknown>; body?: Record<string, unknown> }; children: unknown }) => <div
      className={className}
      data-label-width={styles?.container?.["--vp-form-dialog-label-width"]}
      data-body-padding={styles?.body?.padding}
    >{children as never}</div>,
  };
});

import { Dialog } from "./feedback.js";

describe("Ant Design FormDialog", () => {
  it("uses the shared compact form geometry while retaining the requested component size", () => {
    const html = renderToStaticMarkup(<Dialog open title="补充审批证据" variant="form" size="md" onClose={() => undefined}>内容</Dialog>);
    expect(html).toContain("vp-antd-form-dialog");
    expect(html).toContain('data-label-width="96px"');
    expect(html).toContain('data-body-padding="12"');
  });
});
