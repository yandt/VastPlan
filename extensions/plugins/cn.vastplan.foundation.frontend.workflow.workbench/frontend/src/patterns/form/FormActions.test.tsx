import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  message: (_namespace: string, _key: string, fallback: string) => fallback,
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({
    Stack: ({ children, justify, fill }: { children: unknown; justify?: string; fill?: boolean }) => <div data-justify={justify} data-fill={fill}>{children as never}</div>,
    Button: ({ children, kind, loading, disabled }: { children: unknown; kind?: string; loading?: boolean; disabled?: boolean }) => <button data-kind={kind} data-loading={loading} data-disabled={String(Boolean(disabled))} disabled={disabled}>{children as never}</button>,
    Icon: ({ name }: { name: string }) => <span data-icon={name} />,
  }),
}));

import { FormActions } from "./FormActions.js";

describe("FormActions", () => {
  it("keeps governed footer action groups on one row and puts footer.end before cancel", () => {
    const form = {
      definition: { workflow: { title: "新增", actions: [
        { id: "test", label: "测试连接", icon: "success", placement: "footer.start" },
        { id: "help", label: "帮助", icon: "help", placement: "footer.end" },
      ] } },
      presentation: {}, validation: { valid: true, validating: false }, loading: false, submitting: false,
      requestClose: async () => undefined, runAction: async () => undefined, submit: async () => undefined, setActiveSection: () => undefined,
    };
    const html = renderToStaticMarkup(<FormActions form={form as never} />);
    expect(html).toContain('data-justify="between"');
    expect(html.match(/data-fill="false"/g)).toHaveLength(2);
    expect(html).toContain('data-icon="success"');
    expect(html.indexOf("测试连接")).toBeLessThan(html.indexOf("取消"));
    expect(html.indexOf("帮助")).toBeLessThan(html.indexOf("取消"));
  });

  it("leaves submit available so it can reveal the whole form's validation errors", () => {
    const form = {
      definition: { workflow: { title: "新增" } }, presentation: {}, validation: { valid: false, validating: false }, loading: false, submitting: false,
      requestClose: async () => undefined, runAction: async () => undefined, submit: async () => undefined, setActiveSection: () => undefined,
    };
    const html = renderToStaticMarkup(<FormActions form={form as never} />);
    expect(html).toContain('data-disabled="false">提交');
  });
});
