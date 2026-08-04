import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalI18nProvider } from "@vastplan/ui-primitives";
import { antdPortalUIComponents } from "./index.js";

describe("Ant Design nested form section grid", () => {
  it("applies a section column definition to the fields inside its direct object", () => {
    const Form = antdPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "zh-CN", supportedLocales: ["zh-CN"] }} catalogs={{}}>
      <Form
        schema={{ id: "nested-grid", schema: { type: "object", properties: { options: { type: "object", properties: { user: { type: "string", title: "用户名" }, tlsMode: { type: "string", title: "TLS 模式" } } } } } }}
        value={{ options: {} }}
        onChange={() => undefined}
        presentation={{ navigation: "sections", sections: [{ id: "options", title: "连接选项", columns: 2, fields: ["/options"] }], fields: [{ pointer: "/options", span: 2 }] }}
      />
    </PortalI18nProvider>);

    expect(markup.match(/--vp-form-grid-columns:repeat\(2, minmax\(0, 1fr\)\)/g)).toHaveLength(2);
  });
});
