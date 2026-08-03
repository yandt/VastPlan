import type { PortalAccountSummary } from "./appearance.js";
import { message } from "@vastplan/ui-contract";
import { usePortalI18n } from "./i18n.js";
import { usePortalUI } from "./portal-ui-context.js";
import type { PopoverTriggerProps } from "./primitives.js";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";

/** The avatar is a Shell navigation trigger. Account functions are registered by plugins. */
export function PortalAccountControl({ account, selected, onSelect, trigger }: {
  account: PortalAccountSummary;
  selected?: boolean;
  onSelect?(): void;
  trigger?: PopoverTriggerProps;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const label = i18n.text(message(namespace, "account.open", "打开用户功能"));
  const summary = i18n.text(message(namespace, "account.tooltip", "用户信息：{name}", { name: account.displayName }));
  const initials = account.displayName.trim().slice(0, 1).toUpperCase() || "U";
  return <button
    ref={(node) => trigger?.ref(node)}
    type="button"
    className="vp-account-avatar"
    aria-label={`${label}：${account.displayName}`}
    title={summary}
    aria-pressed={selected}
    aria-expanded={trigger?.["aria-expanded"]}
    aria-controls={trigger?.["aria-controls"]}
    onClick={trigger?.onClick ?? onSelect}
    onKeyDown={trigger?.onKeyDown}
    style={{ width: 32, height: 32, borderRadius: "50%", border: selected ? `2px solid ${ui.theme.tokens.color.primary}` : 0, display: "grid", placeItems: "center", color: "#fff", background: ui.theme.tokens.color.primary, fontWeight: 700, cursor: "pointer", boxSizing: "border-box" }}
  >{initials}</button>;
}
