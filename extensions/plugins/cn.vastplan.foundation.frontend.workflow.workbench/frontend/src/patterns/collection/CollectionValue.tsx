import type { ColumnSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function CollectionValue({ column, value }: { column: ColumnSpec; value: unknown }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const code = String(value ?? "");
  const label = column.valueLabels?.[code];
  const rendered = label !== undefined ? i18n.text(label)
    : column.format === "number" && typeof value === "number" ? i18n.formatNumber(value)
    : (column.format === "date" || column.format === "datetime") && typeof value === "string" && value !== "" ? i18n.formatDate(value)
    : column.format === "boolean" && typeof value === "boolean" ? i18n.text(message(namespace, value ? "value.yes" : "value.no", value ? "是" : "否"))
    : code;
  return column.format === "status" ? <ui.Status tone={column.statusTones?.[code] ?? "neutral"}>{rendered}</ui.Status> : <>{rendered}</>;
}
