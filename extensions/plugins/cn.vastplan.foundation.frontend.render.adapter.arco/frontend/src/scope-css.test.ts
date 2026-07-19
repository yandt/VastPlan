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

  it("支持生产构建压缩后的 Arco 根变量和暗色主题选择器", () => {
    const source = "body{--color-primary-6:22,93,255}body[arco-theme='dark']{--color-primary-6:64,128,255}@media(prefers-color-scheme:dark){body{color:white}}.arco-btn{color:rgb(var(--color-primary-6))}";
    const scoped = scopeDocumentCSS(source);
    expect(scoped).toBe(":host{--color-primary-6:22,93,255}:host([arco-theme='dark']){--color-primary-6:64,128,255}@media(prefers-color-scheme:dark){:host{color:white}}.arco-btn{color:rgb(var(--color-primary-6))}");
  });

  it("不把组件内部的通配符或包含 body 字样的类名改成宿主", () => {
    const source = ".arco-card>*{display:block}.some-body{display:block}";
    expect(scopeDocumentCSS(source)).toBe(source);
  });
});
