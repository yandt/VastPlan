import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalI18nProvider } from "@vastplan/ui-primitives";
import { antdPortalUIComponents } from "./index.js";

describe("Ant Design form field width", () => {
  it("makes value controls fill their governed field column while reserving the label column for booleans", () => {
    const Form = antdPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "zh-CN", supportedLocales: ["zh-CN"] }} catalogs={{}}>
      <Form
        schema={{ id: "field-width", schema: { type: "object", properties: { reason: { type: "string", title: "审批原因" }, acknowledged: { type: "boolean", title: "确认已复核" } } } }}
        value={{ reason: "", acknowledged: false }}
        onChange={() => undefined}
      />
    </PortalI18nProvider>);
    expect(markup).toContain("vp-antd-form-field-value");
    expect(markup).toContain("vp-antd-form-field-boolean");
    expect((markup.match(/确认已复核/g) ?? [])).toHaveLength(1);
    expect(markup).toContain("--vp-form-label-width:96px");
    expect(markup).toContain("margin-inline-start:min(var(--vp-form-label-width,var(--vp-form-label-min-width,112px)),42%)");
    expect(markup).toContain("max-width:42%");
    expect(markup).toContain("padding-inline-end:12px");
    expect(markup).toContain(".vp-antd-form-field-value .ant-form-item-control-input-content&gt;div{width:100%;min-width:0}");
    expect(markup).toContain(".vp-antd-form-field-boolean .ant-form-item-control-input-content{justify-content:flex-start!important}");
  });
});
