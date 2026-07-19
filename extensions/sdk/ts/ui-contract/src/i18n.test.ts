import { describe, expect, it } from "vitest";
import { canonicalLocale, localeDirection, message, resolveLocale } from "./i18n";

describe("framework-neutral locale contract", () => {
  const policy = { defaultLocale: "zh-CN", supportedLocales: ["zh-CN", "en-US", "ar-EG"] };

  it("canonicalizes BCP-47 tags and resolves exact, language, then default matches", () => {
    expect(canonicalLocale("en-us")).toBe("en-US");
    expect(resolveLocale(policy, ["en-GB"])).toBe("en-US");
    expect(resolveLocale(policy, ["not_a_locale"])).toBe("zh-CN");
  });

  it("keeps direction and message references independent from a UI framework", () => {
    expect(localeDirection("ar-EG")).toBe("rtl");
    expect(localeDirection("zh-CN")).toBe("ltr");
    expect(message("cn.vastplan.test", "welcome", "欢迎 {name}", { name: "Ada" })).toEqual({ namespace: "cn.vastplan.test", key: "welcome", fallback: "欢迎 {name}", values: { name: "Ada" } });
  });
});
