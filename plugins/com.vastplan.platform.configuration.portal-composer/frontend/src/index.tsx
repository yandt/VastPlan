import { useState } from "react";
import type { FormSchema } from "@vastplan/portal-ui";
import { usePortalUI } from "@vastplan/portal-ui";

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
  title: "门户组合草稿",
  fields: [
    { key: "name", type: "text", title: "名称", validation: { required: true } },
    { key: "route", type: "text", title: "访问路径", help: "必须以 / 开始", validation: { required: true, pattern: "^/" } },
    { key: "plugins", type: "array", title: "应用功能插件", help: "这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理" }
  ]
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
  return <ui.Page title="门户与插件组合"><ui.Panel title="草稿"><ui.FormRenderer schema={portalCompositionSchema} value={value} onChange={setValue} /><ui.Button onClick={createDraft} loading={saving}>创建草稿</ui.Button></ui.Panel></ui.Page>;
}

export default {
  register(context: { addRoute(route: { path: string; pluginID: string }): void; addMenu(item: { id: string; title: string; route: string }): void }) {
    context.addRoute({ path: "/settings/portals", pluginID: "com.vastplan.platform.configuration.portal-composer" });
    context.addMenu({ id: "platform.portal-composer", title: "系统配置", route: "/settings/portals" });
  },
};
