import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  componentSizeRecipes: { formDialog: { xs: { contentGap: 4 }, sm: { contentGap: 8 }, md: { contentGap: 8 }, lg: { contentGap: 16 } } },
  message: (_namespace: string, _key: string, fallback: string) => fallback,
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({
    Stack: ({ children }: { children: unknown }) => <div>{children as never}</div>,
    FormRenderer: ({ size }: { size?: string }) => <span data-control-size={size} />,
    Skeleton: () => <span>loading</span>,
    ErrorState: () => null,
    Status: () => null,
  }),
}));

import { FormContent, formDialogControlSize } from "./FormContent.js";

const form = {
  definition: { workflow: {} }, presentation: {}, controlSize: "md", schema: { id: "test", schema: { type: "object" } },
  value: {}, change: () => undefined, submitting: false, fieldErrors: {}, context: {}, validation: { valid: true, validating: false, errors: {}, issues: [] },
  setValidation: () => undefined, setActiveSection: () => undefined,
};

describe("FormDialog control size", () => {
  it("reduces every FormDialog control size by one level without going below xs", () => {
    expect(["xs", "sm", "md", "lg"].map((size) => formDialogControlSize(size as never))).toEqual(["xs", "xs", "sm", "md"]);
  });

  it("keeps page forms at their declared size and compacts dialog forms", () => {
    expect(renderToStaticMarkup(<FormContent form={form as never} />)).toContain('data-control-size="md"');
    expect(renderToStaticMarkup(<FormContent form={form as never} density="dialog" />)).toContain('data-control-size="sm"');
  });

  it("keeps asynchronous validation silent so form content does not move while typing", () => {
    const validating = { ...form, validation: { ...form.validation, validating: true } };
    const markup = renderToStaticMarkup(<FormContent form={validating as never} density="dialog" />);

    expect(markup).not.toContain("正在校验表单");
  });
});
