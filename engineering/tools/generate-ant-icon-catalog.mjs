import { createHash } from "node:crypto";
import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { basename, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const sourceRoot = resolve(requiredOption("--source-root"));
const outputRoot = resolve(requiredOption("--out"));
const shardCount = 27;
const files = (await readdir(sourceRoot)).filter((name) => /(?:Outlined|Filled|TwoTone)\.js$/.test(name)).sort();
if (files.length !== 846) throw new Error(`Ant Design 图标数量漂移: ${files.length}`);

const entries = [];
for (const file of files) {
  const definition = (await import(pathToFileURL(resolve(sourceRoot, file)).href)).default;
  if (!definition || typeof definition.name !== "string" || !["outlined", "filled", "twotone"].includes(definition.theme)) {
    throw new Error(`Ant Design 图标定义无效: ${file}`);
  }
  const name = `${definition.name}-${definition.theme === "twotone" ? "two-tone" : definition.theme}`;
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(name)) throw new Error(`Ant Design 图标名称无效: ${file}/${name}`);
  entries.push(Object.freeze({ name, component: basename(file, ".js"), sourceName: definition.name, theme: definition.theme, shard: shardIndex(name) }));
}
entries.sort((left, right) => left.name.localeCompare(right.name));
if (new Set(entries.map((entry) => entry.name)).size !== entries.length) throw new Error("Ant Design 图标目录名称重复");

await rm(resolve(outputRoot, "generated"), { recursive: true, force: true });
await mkdir(resolve(outputRoot, "generated/names"), { recursive: true });
await mkdir(resolve(outputRoot, "generated/shards"), { recursive: true });
for (let index = 0; index < shardCount; index += 1) {
  const shard = entries.filter((entry) => entry.shard === index);
  await writeFile(resolve(outputRoot, `generated/names/shard-${pad(index)}.ts`), namesModule(index, shard));
  await writeFile(resolve(outputRoot, `generated/shards/shard-${pad(index)}.ts`), glyphShardModule(shard));
}
await writeFile(resolve(outputRoot, "generated/catalog.ts"), catalogModule());
await writeFile(resolve(outputRoot, "generated/loaders.ts"), loadersModule().replace(
  "    shard = shardLoaders[index]();",
  "    shard = shardLoaders[index]().catch((error) => {\n      shardCache.delete(index);\n      throw error;\n    });",
));
await writeFile(resolve(outputRoot, "generated/semantic.ts"), semanticModule(entries));
await writeFile(resolve(outputRoot, "generated/manifest.json"), `${JSON.stringify(manifest(entries), null, 2)}\n`);
console.log(`已生成 ${entries.length} 个 Ant Design 图标，${shardCount} 个延迟分片`);

function requiredOption(name) {
  const index = process.argv.indexOf(name);
  if (index < 0 || !process.argv[index + 1]) throw new Error(`缺少参数 ${name}`);
  return process.argv[index + 1];
}

function shardIndex(name) {
  let hash = 0x811c9dc5;
  for (let index = 0; index < name.length; index += 1) {
    hash ^= name.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash % shardCount;
}

function pad(value) { return String(value).padStart(2, "0"); }

function namesModule(index, values) {
  const entries = values.map(({ name, component, sourceName, theme }) => `  ${JSON.stringify({ name, component, sourceName, theme })},`).join("\n");
  return `import type { IconCatalogEntry } from "../../types.js";\n\nexport const shard${pad(index)}Entries = Object.freeze([\n${entries}\n] as const) satisfies readonly IconCatalogEntry[];\nexport type Shard${pad(index)}Name = (typeof shard${pad(index)}Entries)[number]["name"];\n`;
}

function glyphShardModule(values) {
  const imports = values.map((entry, index) => `import icon${index} from "@ant-design/icons-svg/es/asn/${entry.component}.js";`).join("\n");
  const definitions = values.map((entry, index) => `  ${JSON.stringify(entry.name)}: normalizeAntIcon(icon${index}),`).join("\n");
  return `import { normalizeAntIcon } from "../../normalize.js";\nimport type { IconGlyphDefinition } from "../../types.js";\n${imports}\n\nconst glyphs: Readonly<Record<string, IconGlyphDefinition>> = Object.freeze({\n${definitions}\n});\n\nexport default glyphs;\n`;
}

function catalogModule() {
  const imports = Array.from({ length: shardCount }, (_, index) => `import { shard${pad(index)}Entries } from "./names/shard-${pad(index)}.js";\nimport type { Shard${pad(index)}Name } from "./names/shard-${pad(index)}.js";`).join("\n");
  const types = Array.from({ length: shardCount }, (_, index) => `Shard${pad(index)}Name`).join(" | ");
  const spreads = Array.from({ length: shardCount }, (_, index) => `...shard${pad(index)}Entries`).join(", ");
  return `${imports}\n\nexport type IconCatalogName = ${types};\nexport const iconCatalogEntries = Object.freeze([${spreads}].map((entry) => Object.freeze(entry)));\nexport const iconCatalogNames: readonly IconCatalogName[] = Object.freeze(iconCatalogEntries.map((entry) => entry.name));\n`;
}

function loadersModule() {
  const loaders = Array.from({ length: shardCount }, (_, index) => `  () => import("./shards/shard-${pad(index)}.js").then((module) => module.default),`).join("\n");
  return `import { iconCatalogNames } from "./catalog.js";\nimport type { IconCatalogName } from "./catalog.js";\nimport type { IconGlyphDefinition } from "../types.js";\n\nconst knownNames = new Set<string>(iconCatalogNames);\nconst shardLoaders = Object.freeze([\n${loaders}\n]);\nconst shardCache = new Map<number, Promise<Readonly<Record<string, IconGlyphDefinition>>>>();\n\nexport async function loadIconGlyph(name: IconCatalogName): Promise<IconGlyphDefinition> {\n  if (!knownNames.has(name)) throw new Error(\`未知图标目录名称: \${name}\`);\n  const index = shardIndex(name);\n  let shard = shardCache.get(index);\n  if (shard === undefined) {\n    shard = shardLoaders[index]();\n    shardCache.set(index, shard);\n  }\n  const glyph = (await shard)[name];\n  if (glyph === undefined) throw new Error(\`图标目录分片不完整: \${name}\`);\n  return glyph;\n}\n\nfunction shardIndex(name: string): number {\n  let hash = 0x811c9dc5;\n  for (let index = 0; index < name.length; index += 1) {\n    hash ^= name.charCodeAt(index);\n    hash = Math.imul(hash, 0x01000193) >>> 0;\n  }\n  return hash % ${shardCount};\n}\n`;
}

function semanticModule(values) {
  const mapping = {
    add: "PlusOutlined", remove: "DeleteOutlined", edit: "EditOutlined", search: "SearchOutlined", settings: "SettingOutlined",
    success: "CheckCircleOutlined", warning: "WarningOutlined", error: "CloseCircleOutlined", info: "InfoCircleOutlined",
    close: "CloseOutlined", menu: "MenuOutlined", import: "ImportOutlined", export: "ExportOutlined", publish: "CloudUploadOutlined",
    refresh: "ReloadOutlined", columns: "ColumnHeightOutlined", visibility: "EyeOutlined", visibilityOff: "EyeInvisibleOutlined",
    drag: "HolderOutlined", copy: "CopyOutlined", download: "DownloadOutlined", upload: "UploadOutlined", more: "MoreOutlined", help: "QuestionCircleOutlined", logout: "LogoutOutlined",
    user: "UserOutlined", sliders: "SlidersOutlined", authentication: "SafetyCertificateOutlined", marketplace: "ShopOutlined", repository: "InboxOutlined",
    resources: "AppstoreOutlined", plugins: "DeploymentUnitOutlined", portal: "LayoutOutlined", database: "DatabaseOutlined", deployment: "ClusterOutlined",
    api: "ApiOutlined", security: "SafetyOutlined", credential: "KeyOutlined", workbench: "TableOutlined", extension: "ExperimentOutlined", folder: "FolderOutlined",
  };
  const components = new Set(values.map((entry) => entry.component));
  for (const component of Object.values(mapping)) if (!components.has(component)) throw new Error(`语义图标不存在: ${component}`);
  const unique = [...new Set(Object.values(mapping))];
  const imports = unique.map((component, index) => `import icon${index} from "@ant-design/icons-svg/es/asn/${component}.js";`).join("\n");
  const indexes = new Map(unique.map((component, index) => [component, index]));
  const records = Object.entries(mapping).map(([name, component]) => `  ${JSON.stringify(name)}: normalizeAntIcon(icon${indexes.get(component)}),`).join("\n");
  return `import type { SemanticIconName } from "@vastplan/ui-contract";\nimport { normalizeAntIcon } from "../normalize.js";\nimport type { IconGlyphDefinition } from "../types.js";\n${imports}\n\nconst glyphs: Readonly<Record<SemanticIconName, IconGlyphDefinition>> = Object.freeze({\n${records}\n});\n\nexport function semanticIconGlyph(name: SemanticIconName): IconGlyphDefinition { return glyphs[name]; }\n`;
}

function manifest(values) {
  const sourceFiles = values.map(({ component }) => `${component}.js`).sort();
  const digest = createHash("sha256").update(sourceFiles.join("\n")).digest("hex");
  return {
    schemaVersion: "v1", sourcePackage: "@ant-design/icons-svg", sourceVersion: "4.5.0", license: "MIT",
    iconCount: values.length, shardCount, sourceFileListSHA256: digest,
    shards: Array.from({ length: shardCount }, (_, index) => ({ id: pad(index), iconCount: values.filter((entry) => entry.shard === index).length })),
  };
}
