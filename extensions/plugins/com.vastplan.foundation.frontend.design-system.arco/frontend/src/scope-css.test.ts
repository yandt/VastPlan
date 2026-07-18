import { describe, expect, it } from "vitest";
import { scopeDocumentCSS } from "./scope-css";

describe("scopeDocumentCSS", () => {
  it("只转换文档根选择器", () => {
    const source = "body { color:red }\nhtml,\nbody { margin:0 }\n* { outline:none }\n.arco-card-body { padding:1px }\n.arco-card > * { display:block }";
    const scoped = scopeDocumentCSS(source);
    expect(scoped).toContain(":host { color:red }");
    expect(scoped).toContain(":host { margin:0 }");
    expect(scoped).toContain(":host, :host * { outline:none }");
    expect(scoped).toContain(".arco-card-body { padding:1px }");
    expect(scoped).toContain(".arco-card > * { display:block }");
  });
});
