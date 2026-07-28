import { iconCatalogNames } from "./catalog.js";
import type { IconCatalogName } from "./catalog.js";
import type { IconGlyphDefinition } from "../types.js";

const knownNames = new Set<string>(iconCatalogNames);
const shardLoaders = Object.freeze([
  () => import("./shards/shard-00.js").then((module) => module.default),
  () => import("./shards/shard-01.js").then((module) => module.default),
  () => import("./shards/shard-02.js").then((module) => module.default),
  () => import("./shards/shard-03.js").then((module) => module.default),
  () => import("./shards/shard-04.js").then((module) => module.default),
  () => import("./shards/shard-05.js").then((module) => module.default),
  () => import("./shards/shard-06.js").then((module) => module.default),
  () => import("./shards/shard-07.js").then((module) => module.default),
  () => import("./shards/shard-08.js").then((module) => module.default),
  () => import("./shards/shard-09.js").then((module) => module.default),
  () => import("./shards/shard-10.js").then((module) => module.default),
  () => import("./shards/shard-11.js").then((module) => module.default),
  () => import("./shards/shard-12.js").then((module) => module.default),
  () => import("./shards/shard-13.js").then((module) => module.default),
  () => import("./shards/shard-14.js").then((module) => module.default),
  () => import("./shards/shard-15.js").then((module) => module.default),
  () => import("./shards/shard-16.js").then((module) => module.default),
  () => import("./shards/shard-17.js").then((module) => module.default),
  () => import("./shards/shard-18.js").then((module) => module.default),
  () => import("./shards/shard-19.js").then((module) => module.default),
  () => import("./shards/shard-20.js").then((module) => module.default),
  () => import("./shards/shard-21.js").then((module) => module.default),
  () => import("./shards/shard-22.js").then((module) => module.default),
  () => import("./shards/shard-23.js").then((module) => module.default),
  () => import("./shards/shard-24.js").then((module) => module.default),
  () => import("./shards/shard-25.js").then((module) => module.default),
  () => import("./shards/shard-26.js").then((module) => module.default),
]);
const shardCache = new Map<number, Promise<Readonly<Record<string, IconGlyphDefinition>>>>();

export async function loadIconGlyph(name: IconCatalogName): Promise<IconGlyphDefinition> {
  if (!knownNames.has(name)) throw new Error(`未知图标目录名称: ${name}`);
  const index = shardIndex(name);
  let shard = shardCache.get(index);
  if (shard === undefined) {
    shard = shardLoaders[index]().catch((error) => {
      shardCache.delete(index);
      throw error;
    });
    shardCache.set(index, shard);
  }
  const glyph = (await shard)[name];
  if (glyph === undefined) throw new Error(`图标目录分片不完整: ${name}`);
  return glyph;
}

function shardIndex(name: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < name.length; index += 1) {
    hash ^= name.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash % 27;
}
