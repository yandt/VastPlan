import { RequestJSONError, requireJSONObject } from "./request-json";

export function controllerInstallationRequest(value: unknown): Readonly<Record<string, unknown>> {
  const request = exactObject(value, ["version", "target", "change"], ["expectedActiveRevision"]);
  if (request.version !== 1) throw new RequestJSONError("插件安装协议版本无效");
  if (request.expectedActiveRevision !== undefined && (!Number.isSafeInteger(request.expectedActiveRevision) || Number(request.expectedActiveRevision) < 1)) throw new RequestJSONError("活动修订无效");
  const target = exactObject(request.target, ["kernel", "deployment", "unitId"]);
  if (target.kernel !== "backend" || !boundedName(target.deployment, 128) || !boundedName(target.unitId, 128)) throw new RequestJSONError("插件安装目标无效");
  installationChange(request.change);
  return request;
}

export function selfServiceInstallationRequest(value: unknown, target: Readonly<{ kernel: "backend"; deployment: string; unitId: string }>): Readonly<Record<string, unknown>> {
  const request = exactObject(value, ["version", "change"]);
  if (request.version !== 1) throw new RequestJSONError("插件安装协议版本无效");
  const change = installationChange(request.change);
  return Object.freeze({ version: 1, target: Object.freeze({ ...target }), change });
}

function installationChange(value: unknown): Readonly<Record<string, unknown>> {
  const change = exactObject(value, ["action", "pluginId"], ["requirement"]);
  if (!( ["install", "upgrade", "remove"] as unknown[]).includes(change.action) || !pluginID(change.pluginId)) throw new RequestJSONError("插件安装变更无效");
  if (change.action === "remove") {
    if (change.requirement !== undefined) throw new RequestJSONError("卸载请求不能包含版本要求");
  } else {
    const requirement = exactObject(change.requirement, ["pluginId", "constraint"], ["channel", "features"]);
    if (requirement.pluginId !== change.pluginId || typeof requirement.constraint !== "string" || requirement.constraint.length < 1 || requirement.constraint.length > 128) throw new RequestJSONError("插件版本要求无效");
    if (requirement.channel !== undefined && !boundedName(requirement.channel, 32)) throw new RequestJSONError("插件通道无效");
    if (requirement.features !== undefined && (!Array.isArray(requirement.features) || requirement.features.length > 64 || requirement.features.some((item) => !boundedName(item, 128)))) throw new RequestJSONError("插件 Feature 无效");
  }
  return change;
}

function exactObject(value: unknown, required: readonly string[], optional: readonly string[] = []): Readonly<Record<string, unknown>> {
  const object = requireJSONObject(value);
  const allowed = new Set([...required, ...optional]);
  if (required.some((key) => !Object.hasOwn(object, key)) || Object.keys(object).some((key) => !allowed.has(key))) throw new RequestJSONError("插件安装请求字段无效");
  return object;
}

function boundedName(value: unknown, maximum: number): value is string {
  return typeof value === "string" && value.trim() === value && value.length > 0 && value.length <= maximum && !value.includes("/") && !value.includes("\\") && !value.includes("\0");
}

function pluginID(value: unknown): value is string {
  return typeof value === "string" && value.length <= 255 && /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/.test(value);
}
