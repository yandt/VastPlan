import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({ Dialog: ({ description, children }: { description?: string; children: unknown }) => <div data-description={description}>{children as never}</div> }),
}));
vi.mock("./FormActions.js", () => ({ FormActions: () => <span>actions</span> }));
vi.mock("./FormContent.js", () => ({ FormContent: ({ showDescription }: { showDescription?: boolean }) => <span data-show-description={String(showDescription)}>content</span> }));

import { FormDialog } from "./FormDialog.js";

describe("FormDialog description", () => {
  it("moves workflow context into the Dialog header and removes the duplicated body description", () => {
    const form = { definition: { workflow: { title: "编辑", description: "上下文说明" } }, submitting: false, requestClose: async () => undefined };
    const html = renderToStaticMarkup(<FormDialog form={form as never} open />);
    expect(html).toContain('data-description="上下文说明"');
    expect(html).toContain('data-show-description="false"');
  });
});
