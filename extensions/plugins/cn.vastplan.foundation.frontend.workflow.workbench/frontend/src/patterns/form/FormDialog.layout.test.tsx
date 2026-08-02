import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({ Dialog: ({ children, variant }: { children: unknown; variant?: string }) => <div data-variant={variant}>{children as never}</div> }),
}));
vi.mock("./FormActions.js", () => ({ FormActions: () => <span>actions</span> }));
vi.mock("./FormContent.js", () => ({ FormContent: ({ density }: { density?: string }) => <span data-density={density}>content</span> }));

import { FormDialog } from "./FormDialog.js";

describe("FormDialog layout", () => {
  it("selects the shared form Dialog variant and compact content rhythm", () => {
    const form = { definition: { workflow: { title: "编辑" } }, submitting: false, requestClose: async () => undefined };
    const html = renderToStaticMarkup(<FormDialog form={form as never} open />);
    expect(html).toContain('data-variant="form"');
    expect(html).toContain('data-density="dialog"');
  });
});
