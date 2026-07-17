import { useState } from "react";
import type { FormSchema } from "@vastplan/portal-ui";
import { usePortalUI } from "@vastplan/portal-ui";

export const portalCompositionSchema: FormSchema = {
  id: "portal-composition.v1",
  title: "门户组合草稿",
  fields: [
    { key: "name", type: "text", title: "名称", validation: { required: true } },
    { key: "route", type: "text", title: "访问路径", help: "必须以 / 开始", validation: { required: true, pattern: "^/" } },
    { key: "designSystem", type: "select", title: "设计系统", options: [{ label: "Arco Design", value: "com.vastplan.foundation.frontend.design-system.arco" }], validation: { required: true } },
    { key: "plugins", type: "array", title: "功能插件", help: "仅选择已签名且与 Portal UI 契约兼容的插件" }
  ]
};

/** Reference page: it knows only the stable UI SDK, never an Arco/MUI component. */
export function PortalComposerView() {
  const ui = usePortalUI();
  const [value, setValue] = useState<Record<string, unknown>>({ route: "/", designSystem: "com.vastplan.foundation.frontend.design-system.arco" });
  return <ui.Page title="门户与插件组合"><ui.Panel title="草稿"><ui.FormRenderer schema={portalCompositionSchema} value={value} onChange={setValue} /><button type="button" onClick={() => ui.notify({ title: "草稿已校验", content: "提交、审批和发布由受保护的 BFF API 执行。", kind: "success" })}>校验草稿</button></ui.Panel></ui.Page>;
}

export default {
  register(context: { addRoute(route: { path: string; pluginID: string }): void; addMenu(item: { id: string; title: string; route: string }): void }) {
    context.addRoute({ path: "/settings/portals", pluginID: "com.vastplan.platform.configuration.portal-composer" });
    context.addMenu({ id: "platform.portal-composer", title: "系统配置", route: "/settings/portals" });
  },
};
