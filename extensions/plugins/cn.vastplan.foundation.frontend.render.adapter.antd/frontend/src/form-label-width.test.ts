import { describe, expect, it } from "vitest";
import { resolveFormLabelWidth } from "./form-label-width.js";

describe("resolveFormLabelWidth", () => {
  it("uses one width based on the longest value-field label and excludes checkbox text", () => {
    expect(resolveFormLabelWidth({ type: "object", properties: {
      reason: { type: "string", title: "审批原因" },
      revision: { type: "string", title: "当前冻结配置内容摘要" },
      acknowledged: { type: "boolean", title: "确认已复核冻结内容" },
    } }, "md")).toBe(152);
  });
});
