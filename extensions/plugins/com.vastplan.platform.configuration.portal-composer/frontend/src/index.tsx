import { useState } from "react";
import type { FormSchema } from "@vastplan/portal-ui";
import { jsonSchemaDialect, usePortalUI } from "@vastplan/portal-ui";

interface PluginRef {
  id: string;
  version: string;
  channel?: string;
}

export interface ApplicationComposition {
  version: 1;
  revision: number;
  id: string;
  target: { kernel: "frontend" };
  route: string;
  plugins: PluginRef[];
  config: Record<string, unknown>;
}

export const portalCompositionSchema: FormSchema = {
  id: "portal-composition.v1",
  schema: {
    $schema: jsonSchemaDialect,
    title: "门户组合草稿",
    type: "object",
    additionalProperties: false,
    required: ["name", "route", "plugins"],
    properties: {
      name: { type: "string", title: "名称", minLength: 1 },
      route: { type: "string", title: "访问路径", pattern: "^/" },
      plugins: {
        type: "array",
        title: "应用功能插件",
        minItems: 1,
        items: {
          type: "object",
          additionalProperties: false,
          required: ["id", "version"],
          properties: {
            id: { type: "string", title: "插件 ID", pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$" },
            version: { type: "string", title: "精确版本", pattern: "^\\d+\\.\\d+\\.\\d+(?:[-+][0-9A-Za-z.-]+)?$" },
            channel: {
              type: "string",
              title: "发布通道",
              default: "stable",
              oneOf: [
                { const: "stable", title: "稳定版" },
                { const: "preview", title: "预发布" },
              ],
            },
          },
        },
      },
    },
  },
  uiSchema: {
    route: { "ui:help": "必须以 / 开始" },
    plugins: {
      "ui:help": "这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理",
      items: { channel: { "ui:widget": "select" } },
    },
  },
};

export function buildApplicationComposition(value: Record<string, unknown>): ApplicationComposition {
  return {
    version: 1,
    revision: 1,
    id: typeof value.name === "string" && value.name !== "" ? value.name : "portal",
    target: { kernel: "frontend" },
    route: typeof value.route === "string" ? value.route : "/",
    plugins: normalizePluginRefs(value.plugins),
    config: {},
  };
}

function normalizePluginRefs(value: unknown): PluginRef[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((candidate) => {
    if (typeof candidate !== "object" || candidate === null) return [];
    const { id, version, channel } = candidate as Record<string, unknown>;
    if (typeof id !== "string" || typeof version !== "string") return [];
    return [{ id, version, ...(typeof channel === "string" ? { channel } : {}) }];
  });
}

/** Reference page: it knows only the stable UI SDK, never an Arco/MUI component. */
export function PortalComposerView() {
  const ui = usePortalUI();
  const [value, setValue] = useState<Record<string, unknown>>({ route: "/", plugins: [] });
  const [saving, setSaving] = useState(false);
  const [valid, setValid] = useState(false);
  const createDraft = async () => {
    setSaving(true);
    try {
      const csrf = await fetch("/v1/csrf", { credentials: "same-origin" });
      if (!csrf.ok) throw new Error("会话已失效");
      const { token } = await csrf.json() as { token: string };
      const response = await fetch("/v1/portal-drafts", {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": token },
        body: JSON.stringify(buildApplicationComposition(value)),
      });
      if (!response.ok) throw new Error("草稿被控制面拒绝");
      ui.notify({ title: "草稿已创建", content: "可提交给另一位审批人。", kind: "success" });
    } catch (error) {
      ui.notify({ title: "无法创建草稿", content: error instanceof Error ? error.message : "未知错误", kind: "error" });
    } finally { setSaving(false); }
  };
  return <ui.Page title="门户与插件组合"><ui.Panel title="草稿"><ui.FormRenderer schema={portalCompositionSchema} value={value} onChange={setValue} onValidationChange={(result) => setValid(result.valid)} /><ui.Button kind="primary" onClick={createDraft} loading={saving} disabled={!valid}>创建草稿</ui.Button></ui.Panel></ui.Page>;
}

export default {
  register(context: { addRoute(route: { path: string; component: typeof PortalComposerView }): void; addMenu(item: { id: string; title: string; route: string }): void }) {
    context.addRoute({ path: "/settings/portals", component: PortalComposerView });
    context.addMenu({ id: "platform.portal-composer", title: "系统配置", route: "/settings/portals" });
  },
};
