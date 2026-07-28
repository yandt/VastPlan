import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.kernel.frontend";

/** Host-owned trailing command; feature plugins cannot replace its position. */
export function PageHelpButton() {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const label = i18n.text(message(namespace, "page.help", "页面帮助"));
  return <ui.IconButton size="lg" icon="help" label={label} onClick={() => ui.notify({
    title: label,
    content: i18n.text(message(namespace, "page.helpPending", "此页面暂未配置帮助内容。")),
    kind: "info",
  })} />;
}
