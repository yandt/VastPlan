import { describe, expect, it } from "vitest";
import { filterPanelActionSpan, filterPanelColumns, shouldAutoApplyFilterPanel } from "./FilterPanel.js";
import { filterPanelSchema } from "./filter-schema.js";

describe("FilterPanel", () => {
  it("renders each field label as an accessible in-control placeholder", () => {
    const schema = filterPanelSchema([{ id: "status", label: "状态", kind: "select" }]);
    expect(schema.uiSchema).toEqual({ status: { "ui:placeholder": "", "ui:options": { label: false } } });
    expect(schema.uiLocalization).toEqual({ "/status/ui:placeholder": "状态" });
  });

  it("uses direct-query interaction while the desktop grid has fewer than two rows", () => {
    expect(shouldAutoApplyFilterPanel({ fields: [{ id: "name", label: "Name", kind: "text" }, { id: "status", label: "Status", kind: "select" }] })).toBe(true);
  });

  it("requires an explicit query after the default four-column grid reaches two rows", () => {
    expect(shouldAutoApplyFilterPanel({ fields: [{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }] })).toBe(true);
    expect(shouldAutoApplyFilterPanel({ fields: [{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }, { id: "e", label: "E", kind: "text" }] })).toBe(false);
  });

  it("honors explicit apply mode and column counts", () => {
    const fields = [{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }] as const;
    expect(shouldAutoApplyFilterPanel({ fields }, 2)).toBe(false);
    expect(shouldAutoApplyFilterPanel({ fields }, { xs: 1, md: 2, xl: 3 })).toBe(true);
    expect(shouldAutoApplyFilterPanel({ fields, apply: { mode: "explicit" } }, 4)).toBe(false);
    expect(filterPanelColumns({ fields, layout: { columns: 2 } })).toBe(2);
  });

  it("spans the remaining row so actions end in the final column at every breakpoint", () => {
    const fields = [{ id: "a", label: "A", kind: "text" }, { id: "b", label: "B", kind: "text" }, { id: "c", label: "C", kind: "text" }, { id: "d", label: "D", kind: "text" }, { id: "e", label: "E", kind: "text" }] as const;
    expect(filterPanelActionSpan(fields, 4)).toBe(3);
    expect(filterPanelActionSpan(fields, { xs: 1, md: 2, xl: 4 })).toEqual({ xs: 1, sm: 1, md: 1, lg: 1, xl: 3 });
  });
});
