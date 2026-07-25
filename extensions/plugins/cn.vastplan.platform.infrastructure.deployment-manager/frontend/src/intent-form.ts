import { normalizeBackendApplicationIntent } from "@vastplan/composition-planning";
import type {
  BackendApplicationIntent,
  BackendServiceIntent,
  RootPluginSelection,
  ServiceOperationsIntent,
} from "@vastplan/composition-planning";
import type { DeploymentTarget, ServiceRevision } from "@vastplan/platform-admin";
import { jsonSchemaDialect, type FormSchema } from "@vastplan/workbench-sdk";
import { message } from "./localization.js";

export type IntentEditorValue = Readonly<Record<string, unknown>>;

export function serviceIntentSchema(targets: readonly DeploymentTarget[]): FormSchema {
  return {
    id: "backend-application-intent.v1",
    schema: {
      $schema: jsonSchemaDialect,
      title: "Backend Application Intent",
      type: "object",
      additionalProperties: false,
      required: ["deployment", "services"],
      properties: {
        deployment: {
          type: "string", title: "部署目标", minLength: 1,
          oneOf: targets.map((target) => ({ const: target.deploymentName, title: target.deploymentName })),
        },
        services: {
          type: "array", title: "应用服务", minItems: 1, maxItems: 64,
          items: {
            type: "object", additionalProperties: false,
            required: ["serviceClass", "id", "rootPlugins", "replicas"],
            properties: {
              serviceClass: { type: "string", title: "服务分类", default: "application.backend", pattern: "^[a-z][a-z0-9._-]{0,127}$" },
              id: { type: "string", title: "服务 ID", pattern: "^[a-z][a-z0-9._-]{0,127}$" },
              rootPlugins: {
                type: "array", title: "根应用插件", minItems: 1, maxItems: 256,
                items: {
                  type: "object", additionalProperties: false, required: ["pluginId", "version", "channel"],
                  properties: {
                    pluginId: { type: "string", title: "插件 ID", pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$" },
                    version: { type: "string", title: "精确版本", pattern: "^\\d+\\.\\d+\\.\\d+(?:[-+][0-9A-Za-z.-]+)?$" },
                    channel: { type: "string", title: "通道", default: "stable", oneOf: [{ const: "stable", title: "稳定版" }, { const: "preview", title: "预发布" }, { const: "testing", title: "测试版" }] },
                    features: { type: "array", title: "启用 Feature", uniqueItems: true, maxItems: 64, items: { type: "string", pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" } },
                  },
                },
              },
              pluginConfig: { type: "object", title: "插件配置", propertyNames: { pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$" }, additionalProperties: { type: "object" }, default: {} },
              replicas: { type: "integer", title: "实例数", minimum: 1, maximum: 1024, default: 1 },
              autoscaling: {
                type: "object", title: "自动扩缩容", additionalProperties: false,
                required: ["minReplicas", "maxReplicas", "metric", "targetValuePerReplica"],
                properties: {
                  minReplicas: { type: "integer", title: "最小实例数", minimum: 1 }, maxReplicas: { type: "integer", title: "最大实例数", minimum: 1 },
                  metric: { type: "string", title: "指标", pattern: "^[A-Za-z][A-Za-z0-9._-]{0,127}$" }, targetValuePerReplica: { type: "number", title: "单实例目标值", exclusiveMinimum: 0 },
                },
              },
              resources: {
                type: "object", title: "单实例资源请求", additionalProperties: false,
                properties: {
                  cpuMillis: { type: "integer", title: "CPU（毫核）", minimum: 0 }, memoryBytes: { type: "integer", title: "内存（字节）", minimum: 0 }, gpu: { type: "integer", title: "GPU 数量", minimum: 0 },
                },
              },
              nodeSelector: { type: "object", title: "节点标签选择", additionalProperties: { type: "string" }, default: {} },
            },
          },
        },
      },
    },
    uiSchema: {
      deployment: { "ui:widget": "select", "ui:help": "目标及 Platform Profile 由平台运维预授权，不能在此修改。" },
      services: {
        "ui:help": "这里只声明根插件、Feature、非敏感配置及容量意图；依赖、运行策略、状态模型和路由由 Planner 推导。",
        items: {
          rootPlugins: { items: { channel: { "ui:widget": "select" }, features: { "ui:help": "Feature 必须由插件签名 Manifest 预先声明。" } } },
          pluginConfig: { "ui:help": "以插件 ID 为键填写非敏感配置；密码和令牌必须通过凭证配置流程绑定。" },
        },
      },
    },
    localization: {
      "/properties/deployment/title": message("form.deployment", "部署目标"), "/properties/services/title": message("form.services", "应用服务"),
      "/properties/services/items/properties/serviceClass/title": message("form.serviceClass", "服务分类"), "/properties/services/items/properties/id/title": message("form.serviceId", "服务 ID"),
      "/properties/services/items/properties/rootPlugins/title": message("form.rootPlugins", "根应用插件"),
      "/properties/services/items/properties/rootPlugins/items/properties/pluginId/title": message("form.pluginId", "插件 ID"),
      "/properties/services/items/properties/rootPlugins/items/properties/version/title": message("form.version", "精确版本"),
      "/properties/services/items/properties/rootPlugins/items/properties/channel/title": message("form.channel", "通道"),
      "/properties/services/items/properties/rootPlugins/items/properties/channel/oneOf/0/title": message("form.stable", "稳定版"),
      "/properties/services/items/properties/rootPlugins/items/properties/channel/oneOf/1/title": message("form.preview", "预发布"),
      "/properties/services/items/properties/rootPlugins/items/properties/channel/oneOf/2/title": message("form.testing", "测试版"),
      "/properties/services/items/properties/rootPlugins/items/properties/features/title": message("form.features", "启用 Feature"),
      "/properties/services/items/properties/pluginConfig/title": message("form.pluginConfig", "插件配置"),
      "/properties/services/items/properties/replicas/title": message("form.replicas", "实例数"),
      "/properties/services/items/properties/autoscaling/title": message("form.autoscaling", "自动扩缩容"),
      "/properties/services/items/properties/autoscaling/properties/minReplicas/title": message("form.minReplicas", "最小实例数"),
      "/properties/services/items/properties/autoscaling/properties/maxReplicas/title": message("form.maxReplicas", "最大实例数"),
      "/properties/services/items/properties/autoscaling/properties/metric/title": message("form.metric", "指标"),
      "/properties/services/items/properties/autoscaling/properties/targetValuePerReplica/title": message("form.targetValue", "单实例目标值"),
      "/properties/services/items/properties/resources/title": message("form.resources", "单实例资源请求"),
      "/properties/services/items/properties/resources/properties/cpuMillis/title": message("form.cpuMillis", "CPU（毫核）"),
      "/properties/services/items/properties/resources/properties/memoryBytes/title": message("form.memoryBytes", "内存（字节）"),
      "/properties/services/items/properties/resources/properties/gpu/title": message("form.gpu", "GPU 数量"),
      "/properties/services/items/properties/nodeSelector/title": message("form.nodeSelector", "节点标签选择"),
    },
    uiLocalization: {
      "/deployment/ui:help": message("help.deployment", "目标及 Platform Profile 由平台运维预授权，不能在此修改。"),
      "/services/ui:help": message("help.services", "这里只声明根插件、Feature、非敏感配置及容量意图；依赖、运行策略、状态模型和路由由 Planner 推导。"),
      "/services/items/rootPlugins/items/features/ui:help": message("help.features", "Feature 必须由插件签名 Manifest 预先声明。"),
      "/services/items/pluginConfig/ui:help": message("help.config", "以插件 ID 为键填写非敏感配置；密码和令牌必须通过凭证配置流程绑定。"),
    },
  };
}

export function buildBackendIntent(value: IntentEditorValue, revision = 1): BackendApplicationIntent {
  const deployment = text(value.deployment) ?? "deployment";
  return normalizeBackendApplicationIntent({
    version: 1, revision, id: deployment, target: { kernel: "backend" }, metadata: { name: deployment },
    services: Array.isArray(value.services) ? value.services.flatMap(buildServiceIntent) : [],
  });
}

export function intentEditorValue(revision: ServiceRevision): IntentEditorValue {
  if (revision.intent === undefined) return { deployment: revision.deployment, services: [] };
  return {
    deployment: revision.deployment,
    services: revision.intent.services.map((service) => ({
      serviceClass: service.serviceClass, id: service.id,
      rootPlugins: service.rootPlugins.map((selection) => ({ pluginId: selection.ref.pluginId, version: selection.ref.version, channel: selection.ref.channel, features: selection.features ?? [] })),
      pluginConfig: service.pluginConfig ?? {}, replicas: service.operations.replicas,
      ...(service.operations.autoscaling === undefined ? {} : { autoscaling: {
        minReplicas: service.operations.autoscaling.min_replicas, maxReplicas: service.operations.autoscaling.max_replicas,
        metric: service.operations.autoscaling.metric, targetValuePerReplica: service.operations.autoscaling.target_value_per_replica,
      } }),
      ...(service.operations.resources?.requests === undefined ? {} : { resources: {
        cpuMillis: service.operations.resources.requests.cpu_millis, memoryBytes: service.operations.resources.requests.memory_bytes, gpu: service.operations.resources.requests.gpu,
      } }),
      nodeSelector: service.operations.placement?.nodeSelector ?? {},
    })),
  };
}

function buildServiceIntent(value: unknown): BackendServiceIntent[] {
  const item = object(value);
  const id = text(item?.id), serviceClass = text(item?.serviceClass);
  if (item === undefined || id === undefined || serviceClass === undefined) return [];
  const operations: ServiceOperationsIntent = { replicas: positiveInteger(item.replicas) ?? 1 };
  const autoscaling = object(item.autoscaling);
  if (autoscaling !== undefined) operations.autoscaling = {
    min_replicas: positiveInteger(autoscaling.minReplicas) ?? 1, max_replicas: positiveInteger(autoscaling.maxReplicas) ?? 1,
    metric: text(autoscaling.metric) ?? "requests", target_value_per_replica: positiveNumber(autoscaling.targetValuePerReplica) ?? 1,
  };
  const resources = object(item.resources);
  const requests = resources === undefined ? undefined : compactNumbers(resources, ["cpuMillis", "memoryBytes", "gpu"]);
  if (requests !== undefined) operations.resources = { requests: {
    ...(requests.cpuMillis === undefined ? {} : { cpu_millis: requests.cpuMillis }), ...(requests.memoryBytes === undefined ? {} : { memory_bytes: requests.memoryBytes }),
    ...(requests.gpu === undefined ? {} : { gpu: requests.gpu }),
  } };
  const nodeSelector = stringRecord(item.nodeSelector);
  if (nodeSelector !== undefined) operations.placement = { nodeSelector };
  const pluginConfig = nestedRecord(item.pluginConfig);
  return [{ id, serviceClass, rootPlugins: rootPlugins(item.rootPlugins), ...(pluginConfig === undefined ? {} : { pluginConfig }), operations }];
}

function rootPlugins(value: unknown): RootPluginSelection[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((candidate) => {
    const item = object(candidate), pluginId = text(item?.pluginId), version = text(item?.version);
    if (item === undefined || pluginId === undefined || version === undefined) return [];
    const features = strings(item.features);
    return [{ ref: { pluginId, version, channel: text(item.channel) ?? "stable" }, ...(features.length === 0 ? {} : { features }) }];
  });
}

function object(value: unknown): Record<string, unknown> | undefined { return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined; }
function text(value: unknown): string | undefined { return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined; }
function strings(value: unknown): string[] { return Array.isArray(value) ? value.flatMap((item) => { const normalized = text(item); return normalized === undefined ? [] : [normalized]; }).filter((item, index, all) => all.indexOf(item) === index) : []; }
function positiveInteger(value: unknown): number | undefined { return typeof value === "number" && Number.isSafeInteger(value) && value > 0 ? value : undefined; }
function positiveNumber(value: unknown): number | undefined { return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined; }

function compactNumbers(source: Record<string, unknown>, keys: readonly string[]): Record<string, number> | undefined {
  const entries = keys.flatMap((key) => typeof source[key] === "number" && Number.isSafeInteger(source[key]) && (source[key] as number) >= 0 ? [[key, source[key] as number] as const] : []);
  return entries.length === 0 ? undefined : Object.fromEntries(entries);
}

function stringRecord(value: unknown): Record<string, string> | undefined {
  const source = object(value);
  if (source === undefined) return undefined;
  const entries = Object.entries(source).flatMap(([key, item]) => text(item) === undefined ? [] : [[key, text(item)!] as const]);
  return entries.length === 0 ? undefined : Object.fromEntries(entries);
}

function nestedRecord(value: unknown): Record<string, Record<string, unknown>> | undefined {
  const source = object(value);
  if (source === undefined) return undefined;
  const entries = Object.entries(source).flatMap(([key, item]) => object(item) === undefined ? [] : [[key, structuredClone(item as Record<string, unknown>)] as const]);
  return entries.length === 0 ? undefined : Object.fromEntries(entries);
}
