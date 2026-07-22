import { describe, expect, it } from "vitest";
import { firstSelectableTreeNode, hasSelectableTreeNode, initialExpandedTreeNodes, validateRecordTree } from "./tree-model.js";

describe("record tree model", () => {
  const tree = [{ id: "root", title: "Root", disabled: true, children: [{ id: "child", title: "Child", children: [{ id: "leaf", title: "Leaf" }] }] }];
  it("validates unique bounded nodes and derives navigation state", () => {
    const validated = validateRecordTree(tree);
    expect(firstSelectableTreeNode(validated)).toBe("child");
    expect(initialExpandedTreeNodes(validated, 2)).toEqual(["root", "child"]);
    expect(hasSelectableTreeNode(validated, "leaf")).toBe(true);
    expect(hasSelectableTreeNode(validated, "root")).toBe(false);
  });
  it("rejects duplicate IDs", () => {
    expect(() => validateRecordTree([{ id: "same", title: "A" }, { id: "same", title: "B" }])).toThrow("重复");
  });
});
