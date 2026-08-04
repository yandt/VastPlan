import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("antd", async (importOriginal) => {
  const actual = await importOriginal<typeof import("antd")>();
  return {
    ...actual,
    Modal: ({ centered, width, styles, children }: { centered?: boolean; width?: string | number; styles?: { container?: Record<string, unknown>; body?: Record<string, unknown>; footer?: Record<string, unknown> }; children: unknown }) => <div
      data-centered={centered}
      data-width={width}
      data-container-max-height={styles?.container?.maxHeight}
      data-body-max-height={styles?.body?.maxHeight}
      data-row-gap={styles?.container?.["--vp-form-dialog-row-gap"]}
      data-section-gap={styles?.container?.["--vp-form-dialog-section-gap"]}
      data-item-margin={styles?.container?.["--vp-form-dialog-item-margin"]}
      data-footer-flex={styles?.footer?.flex}
    >{children as never}</div>,
  };
});

import { Dialog } from "./feedback.js";

describe("Ant Design FormDialog viewport geometry", () => {
  it("keeps a screen gutter and passes the compact rhythm to dynamic form descendants", () => {
    const html = renderToStaticMarkup(<Dialog open title="新增数据库连接" variant="form" width="lg" size="md" footer={<span>保存</span>} onClose={() => undefined}>内容</Dialog>);
    expect(html).toContain('data-centered="true"');
    expect(html).toContain('data-width="min(960px, calc(100vw - 48px))"');
    expect(html).toContain('data-container-max-height="95vh"');
    expect(html).toContain('data-body-max-height="calc(95vh - 144px)"');
    expect(html).toContain('data-row-gap="8px"');
    expect(html).toContain('data-section-gap="16px"');
    expect(html).toContain('data-item-margin="0px"');
    expect(html).toContain('data-footer-flex="0 0 auto"');
  });

  it("does not change the positioning or viewport maximum of ordinary dialogs", () => {
    const html = renderToStaticMarkup(<Dialog open title="确认操作" onClose={() => undefined}>内容</Dialog>);
    expect(html).toContain('data-centered="false"');
    expect(html).toContain('data-container-max-height="90vh"');
    expect(html).toContain('data-body-max-height="calc(90vh - 144px)"');
  });
});
