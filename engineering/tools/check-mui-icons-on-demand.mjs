import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";

const sourceFile = resolve("extensions/plugins/cn.vastplan.foundation.frontend.render.adapter.mui/frontend/src/native-icons.tsx");
const bundleFile = resolve(process.env.MUI_BUNDLE_FILE ?? "extensions/plugins/cn.vastplan.foundation.frontend.render.adapter.mui/frontend/dist/index.js");
const source = readFileSync(sourceFile, "utf8");

if (/from\s+["']@mui\/icons-material["']/.test(source) || /import\s+\*\s+as\s+\w+\s+from\s+["']@mui\/icons-material/.test(source)) {
  throw new Error("Material Icons 必须按图标子路径导入，禁止使用 @mui/icons-material barrel");
}

const imports = [...source.matchAll(/from\s+["']@mui\/icons-material\/([A-Za-z0-9]+)["']/g)].map((match) => match[1]);
if (imports.length === 0 || new Set(imports).size !== imports.length) {
  throw new Error("Material Icons 原生目录缺少按需子路径导入或存在重复导入");
}
if (imports.length > 24) throw new Error(`Material Icons 原生目录一次引入 ${imports.length} 个图标，超过 24 个治理上限`);

const maxBundleBytes = Number.parseInt(process.env.MUI_BUNDLE_MAX_BYTES ?? "850000", 10);
const bundleBytes = statSync(bundleFile).size;
if (bundleBytes > maxBundleBytes) {
  throw new Error(`MUI Renderer bundle ${bundleBytes} 字节超过 ${maxBundleBytes} 字节；检查是否误引入完整图标库`);
}

console.log(`Material Icons 按需加载校验通过: ${imports.length} 个子路径图标、${bundleBytes}/${maxBundleBytes} 字节`);
