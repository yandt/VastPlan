import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({ Dialog: ({ children, height, contentOverflow }: { children: unknown; height?: number; contentOverflow?: string }) => <div data-height={height} data-overflow={contentOverflow}>{children as never}</div> }),
}));
vi.mock("./FormActions.js", () => ({ FormActions: () => <span>actions</span> }));
vi.mock("./FormContent.js", () => ({ FormContent: () => <span>content</span> }));

import { FormDialog } from "./FormDialog.js";

describe("FormDialog", () => {
  it("delegates bounded height and body-only scrolling to the Dialog adapter", () => {
    const form = { definition: { workflow: { title: "编辑", dialogHeight: 640 } }, submitting: false, requestClose: async () => undefined };
    const html = renderToStaticMarkup(<FormDialog form={form as never} open />);
    expect(html).toContain('data-height="640"');
    expect(html).toContain('data-overflow="scroll"');
  });
});
