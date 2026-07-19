import { describe, expect, it } from "vitest";
import { localizeJSONSchema, message, translate } from "@vastplan/ui-primitives";

const catalogs = {
  "cn.vastplan.test": {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { greeting: "你好，{name}" },
      "en-US": { greeting: "Hello, {name}" },
    },
  },
};

describe("Portal locale runtime", () => {
  it("resolves plugin-scoped messages and safely interpolates text values", () => {
    const descriptor = message("cn.vastplan.test", "greeting", "你好", { name: "Ada" });
    expect(translate(descriptor, "en-GB", catalogs)).toBe("Hello, Ada");
    expect(translate(message("cn.vastplan.test", "missing", "Fallback {name}", { name: "<b>" }), "en-US", catalogs)).toBe("Fallback <b>");
  });

  it("localizes a cloned JSON Schema through governed JSON pointers", () => {
    const source = { type: "object", properties: { name: { type: "string", title: "名称" } } };
    const localized = localizeJSONSchema(source, { "/properties/name/title": message("cn.vastplan.test", "greeting", "名称", { name: "field" }) }, (value) => translate(value, "en-US", catalogs));
    expect((localized.properties as Record<string, Record<string, unknown>>).name.title).toBe("Hello, field");
    expect(source.properties.name.title).toBe("名称");
  });
});
