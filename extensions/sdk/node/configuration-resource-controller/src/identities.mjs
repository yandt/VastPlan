import { createHash } from "node:crypto";

export const CONFIGURATION_RESOURCE_PROTOCOL = "configuration.resource.v1";
export const CONFIGURATION_RESOURCE_EXTENSION_POINT = "configuration.resource-controller";

export function configurationResourceControllerCapability(pluginId) {
  const value = requiredIdentity(pluginId, "插件身份");
  return `configuration.resource.${createHash("sha256").update(value).digest("hex").slice(0, 32)}`;
}

export function configurationResourceCollectionId(pluginId, collectionId) {
  const plugin = requiredIdentity(pluginId, "插件身份");
  const collection = requiredIdentity(collectionId, "集合身份");
  const framed = `${Buffer.byteLength(plugin)}:${plugin}\n${Buffer.byteLength(collection)}:${collection}\n`;
  return `cfgc_${createHash("sha256").update(framed).digest("hex").slice(0, 24)}`;
}

function requiredIdentity(value, name) {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`配置资源控制器缺少${name}`);
  return value.trim();
}
