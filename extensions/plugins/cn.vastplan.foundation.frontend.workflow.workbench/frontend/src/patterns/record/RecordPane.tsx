import type { ActionSpec, RecordDetailSpec, RecordFieldSpec } from "@vastplan/ui-contract";
import { message, usePortalI18n, usePortalUI } from "@vastplan/ui-primitives";
import type { WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { CollectionValue } from "../collection/CollectionValue.js";
import { CollectionFormWorkflow } from "../form/CollectionFormWorkflow.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function RecordPane({ detail, record, editor, actions, loading, failure, onRetry, onAction, onDirtyChange, onBack }: {
  detail: RecordDetailSpec;
  record?: Readonly<Record<string, unknown>>;
  editor?: WorkbenchFormDefinition;
  actions: readonly ActionSpec[];
  loading: boolean;
  failure?: string;
  onRetry(): void;
  onAction(action: ActionSpec): void;
  onDirtyChange(dirty: boolean): void;
  onBack?(): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  if (failure !== undefined) return <ui.ErrorState title={failure} retry={onRetry} />;
  if (loading) return <ui.Skeleton rows={7} />;
  if (record === undefined) return <ui.EmptyState title={i18n.text(detail.emptyTitle ?? message(namespace, "record.empty", "请选择一条记录"))} />;
  const title = String(record[detail.titleKey] ?? "");
  const subtitle = detail.subtitleKey === undefined ? undefined : String(record[detail.subtitleKey] ?? "");
  const status = detail.status === undefined ? undefined : String(record[detail.status.labelKey] ?? "");
  const selected = [record];
  return <ui.Stack gap="md">
    <ui.Stack direction="row" gap="sm" align="center" justify="between" wrap>
      <ui.Stack direction="row" gap="sm" align="center" wrap>
        {onBack === undefined ? null : <ui.Button kind="text" onClick={onBack}>{i18n.text(message(namespace, "record.back", "返回列表"))}</ui.Button>}
        <div><strong>{title}</strong>{subtitle === undefined || subtitle === "" ? null : <div style={{ marginTop: 4, color: ui.theme.tokens.color.mutedText }}>{subtitle}</div>}</div>
        {status === undefined || status === "" ? null : <ui.Status tone={recordTone(detail.status?.toneKey === undefined ? undefined : record[detail.status.toneKey])}>{status}</ui.Status>}
      </ui.Stack>
      <ui.Stack direction="row" gap="xs" align="center" wrap>{actions.map((action) => <ui.Button key={action.id} kind={action.tone === "danger" ? "danger" : action.tone === "primary" ? "primary" : "secondary"} onClick={() => onAction(action)}>{action.icon === undefined ? null : <ui.Icon name={action.icon} />} {i18n.text(action.label)}</ui.Button>)}</ui.Stack>
    </ui.Stack>
    {editor === undefined ? detail.sections.map((section) => <ui.Panel key={section.id} title={section.title === undefined ? undefined : i18n.text(section.title)}>
      {section.description === undefined ? null : <p>{i18n.text(section.description)}</p>}
      <ui.Descriptions columns={{ xs: 1, sm: 1, md: section.columns ?? 2, lg: section.columns ?? 2, xl: section.columns ?? 2 }} items={section.fields.map((field) => ({ id: field.key, label: i18n.text(field.label), value: <RecordField field={field} value={record[field.key]} /> }))} />
    </ui.Panel>) : <CollectionFormWorkflow definition={editor} selected={selected} open onRefresh={onRetry} onDirtyChange={onDirtyChange} />}
  </ui.Stack>;
}

function RecordField({ field, value }: { field: RecordFieldSpec; value: unknown }) {
  return <CollectionValue column={{ key: field.key, label: field.label, format: field.format, valueLabels: field.valueLabels, statusTones: field.statusTones }} value={value} />;
}

function recordTone(value: unknown): "neutral" | "info" | "success" | "warning" | "error" {
  return value === "info" || value === "success" || value === "warning" || value === "error" ? value : "neutral";
}
