import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  message: (_namespace: string, _key: string, fallback: string) => fallback,
  usePortalI18n: () => ({ text: (value: string) => value }),
  usePortalUI: () => ({
    Stack: ({ children, justify }: { children: unknown; justify?: string }) => <div data-justify={justify}>{children as never}</div>,
    Button: ({ children, kind, loading, disabled }: { children: unknown; kind?: string; loading?: boolean; disabled?: boolean }) => <button data-kind={kind} data-loading={loading} disabled={disabled}>{children as never}</button>,
    Icon: ({ name }: { name: string }) => <span data-icon={name} />,
  }),
}));

import { FormActions } from "./FormActions.js";

describe("FormActions", () => {
  it("renders governed footer.start actions separately from submit controls", () => {
    const form = {
      definition: { workflow: { title: "新增", actions: [{ id: "test", label: "测试连接", icon: "success", placement: "footer.start" }] } },
      presentation: {}, validation: { valid: true, validating: false }, loading: false, submitting: false,
      requestClose: async () => undefined, runAction: async () => undefined, submit: async () => undefined, setActiveSection: () => undefined,
    };
    const html = renderToStaticMarkup(<FormActions form={form as never} />);
    expect(html).toContain('data-justify="between"');
    expect(html).toContain('data-icon="success"');
    expect(html.indexOf("测试连接")).toBeLessThan(html.indexOf("取消"));
  });
});
