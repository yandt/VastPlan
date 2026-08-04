import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalI18nProvider } from "@vastplan/ui-primitives";
import { DurationWidget, antdDurationWidgetCSS, durationDisplayValue, durationStorageValue } from "./duration-widget.js";

describe("Ant Design duration widget", () => {
  it("converts fixed duration units without changing the canonical storage unit", () => {
    expect(durationDisplayValue(90_000, "millisecond", "second")).toBe(90);
    expect(durationDisplayValue(7_200_000, "millisecond", "hour")).toBe(2);
    expect(durationStorageValue(1.5, "millisecond", "minute")).toBe(90_000);
    expect(durationStorageValue(1, "millisecond", "month")).toBe(2_592_000_000);
  });

  it("renders the caller-selected default unit without unrelated units", () => {
    const html = renderDurationWidget([]);
    expect(html).toContain('aria-valuenow="10"');
    expect(html).toContain("秒");
    expect(html).not.toContain("小时");
    expect(html).toContain("vp-antd-duration-input");
    expect(html).toContain("ant-select-borderless");
    expect(html).toContain("width:max-content;min-width:56px");
    expect(html).toContain("text-align:right");
    expect(html).toContain("ant-input-number-borderless");
    expect(html).not.toContain("ant-input-number-suffix");
    expect(html).not.toContain("ant-space-compact");
    expect(html).not.toContain("data-invalid");
    expect(html.indexOf("ant-input-number")).toBeLessThan(html.indexOf("ant-select"));
  });

  it("scopes the error border to the duration field that owns the error", () => {
    expect(renderDurationWidget(["输入无效"])).toContain('data-invalid="true"');
    expect(antdDurationWidgetCSS).toContain('.vp-antd-duration-input[data-invalid="true"]');
    expect(antdDurationWidgetCSS).not.toContain(".ant-form-item-has-error .vp-antd-duration-input");
  });
});

function renderDurationWidget(rawErrors: string[]) {
  return renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "zh-CN", supportedLocales: ["zh-CN"] }} catalogs={{}}><DurationWidget
      id="timeout"
      name="timeout"
      label="连接超时"
      value={10_000}
      required={false}
      disabled={false}
      readonly={false}
      autofocus={false}
      hideLabel={false}
      placeholder=""
      rawErrors={rawErrors}
      rawDescription={undefined}
      rawHelp={undefined}
      options={{ vastplanDuration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" } }}
      schema={{ type: "integer" }}
      uiSchema={{}}
      formContext={{}}
      registry={{} as never}
      onChange={() => undefined}
      onBlur={() => undefined}
      onFocus={() => undefined}
    /></PortalI18nProvider>);
}
