#!/usr/bin/env node
import { readFileSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, normalize, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { transform } from "esbuild";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const frontend = join(root, "extensions/plugins/com.vastplan.foundation.frontend.design-system.arco/frontend");
const source = join(frontend, "src");
const componentsFile = join(source, "arco-components.ts");
const stylesFile = join(source, "arco-styles.ts");
const scopeFile = join(source, "scope-css.ts");
const bundleFile = join(frontend, "dist/index.js");
const require = createRequire(import.meta.url);
const arcoRoot = dirname(require.resolve("@arco-design/web-react/package.json", { paths: [frontend] }));

const componentsSource = readFileSync(componentsFile, "utf8");
const componentNames = new Set(
  [...componentsSource.matchAll(/@arco-design\/web-react\/es\/([^";]+)"/g)].map((match) => match[1]),
);

const expectedStyles = new Set();
const visitedStyleLoaders = new Set();

function collectStyleClosure(file) {
  const normalized = normalize(file);
  if (visitedStyleLoaders.has(normalized)) return;
  visitedStyleLoaders.add(normalized);
  for (const match of readFileSync(normalized, "utf8").matchAll(/import\s+["']([^"']+)["']/g)) {
    const dependency = resolve(dirname(normalized), match[1]);
    if (dependency.endsWith(".css")) expectedStyles.add(relative(arcoRoot, dependency));
    else collectStyleClosure(dependency);
  }
}

for (const component of componentNames) {
  collectStyleClosure(join(arcoRoot, "es", component, "style/css.js"));
}

const declaredStyles = new Set(
  [...readFileSync(stylesFile, "utf8").matchAll(/@arco-design\/web-react\/(es\/[^";]+\.css)"/g)]
    .map((match) => match[1]),
);
const missing = [...expectedStyles].filter((style) => !declaredStyles.has(style)).sort();
const stale = [...declaredStyles].filter((style) => !expectedStyles.has(style)).sort();
if (missing.length > 0 || stale.length > 0) {
  if (missing.length > 0) console.error(`Arco 按需样式缺失:\n${missing.join("\n")}`);
  if (stale.length > 0) console.error(`Arco 按需样式已无组件引用:\n${stale.join("\n")}`);
  process.exit(1);
}

for (const file of [componentsFile, stylesFile, join(source, "index.tsx"), join(source, "json-schema-form.tsx")]) {
  const content = readFileSync(file, "utf8");
  if (content.includes('from "@arco-design/web-react"') || content.includes("dist/css/arco.css")) {
    console.error(`禁止 Arco 全量入口: ${relative(root, file)}`);
    process.exit(1);
  }
}

const maxBundleBytes = Number.parseInt(process.env.ARCO_BUNDLE_MAX_BYTES ?? "1700000", 10);
const bundleBytes = statSync(bundleFile).size;
const bundleSource = readFileSync(bundleFile, "utf8");
for (const selector of [".arco-btn", ".arco-form-item", ".arco-picker", ".arco-table", ".arco-color-picker"]) {
  if (!bundleSource.includes(selector)) {
    console.error(`Arco 插件制品缺少必要样式: ${selector}`);
    process.exit(1);
  }
}
const scopeModuleSource = await transform(readFileSync(scopeFile, "utf8"), { loader: "ts", format: "esm", minify: true });
const scopeModule = await import(`data:text/javascript;base64,${Buffer.from(scopeModuleSource.code).toString("base64")}`);
const arcoBaseCSS = await transform(readFileSync(join(arcoRoot, "es/style/index.css"), "utf8"), { loader: "css", minify: true });
const scopedProductionCSS = scopeModule.scopeDocumentCSS(arcoBaseCSS.code);
if (!scopedProductionCSS.includes(":host{--red-1:") || scopedProductionCSS.includes("body{--red-1:")) {
  console.error("Arco 插件制品未将压缩后的文档根主题变量绑定到 Shadow DOM host");
  process.exit(1);
}
if (!scopedProductionCSS.includes(":host([arco-theme=")) {
  console.error("Arco 插件制品未将暗色主题选择器绑定到 Shadow DOM host");
  process.exit(1);
}
if (!Number.isSafeInteger(maxBundleBytes) || maxBundleBytes <= 0) {
  console.error("ARCO_BUNDLE_MAX_BYTES 必须是正整数");
  process.exit(2);
}
if (bundleBytes > maxBundleBytes) {
  console.error(`Arco 插件制品 ${bundleBytes} 字节，超过预算 ${maxBundleBytes} 字节`);
  process.exit(1);
}

console.log(`Arco 按需加载校验通过: ${componentNames.size} 个组件、${declaredStyles.size} 个样式、${bundleBytes}/${maxBundleBytes} 字节`);
