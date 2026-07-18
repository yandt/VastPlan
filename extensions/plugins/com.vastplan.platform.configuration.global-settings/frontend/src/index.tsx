import { useCallback, useEffect, useMemo, useState } from "react";
import { createBrowserPlatformAdminClient, type PlatformAdminClient, type Setting } from "@vastplan/platform-admin";
import { jsonSchemaDialect, usePortalUI, type FormSchema } from "@vastplan/portal-ui";

const schema: FormSchema = {
  id: "platform-setting.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["key", "value"],
    properties: {
      key: { type: "string", title: "设置键", minLength: 1, maxLength: 320 },
      value: { type: "string", title: "JSON 值", minLength: 1 },
    },
  },
  uiSchema: { value: { "ui:widget": "textarea", "ui:help": "保存前会校验为有效 JSON；禁止保存密码和令牌。" } },
};

type Editor = { key?: string; value?: string };

export function GlobalSettingsView({ client: supplied }: { client?: PlatformAdminClient } = {}) {
  const ui = usePortalUI();
  const client = useMemo(() => supplied ?? createBrowserPlatformAdminClient(), [supplied]);
  const [items, setItems] = useState<Setting[]>([]);
  const [editor, setEditor] = useState<Editor>({ value: "{}" });
  const [selected, setSelected] = useState<Setting>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setBusy(true);
    try { setItems(await client.listSettings()); setError(undefined); }
    catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  }, [client]);
  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (editor.key === undefined || editor.value === undefined) return;
    let value: unknown;
    try { value = JSON.parse(editor.value); } catch { setError("设置值不是有效 JSON"); return; }
    setBusy(true);
    try {
      await client.putSetting(editor.key, value, selected?.version);
      ui.notify({ title: "设置已保存", kind: "success" });
      setSelected(undefined); setEditor({ value: "{}" }); await load();
    } catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  };

  const remove = async () => {
    if (selected === undefined || !await ui.confirm({ title: "删除设置", content: `确认删除 ${selected.key}？版本不匹配时系统会拒绝。` })) return;
    setBusy(true);
    try { await client.deleteSetting(selected.key, selected.version); setSelected(undefined); setEditor({ value: "{}" }); await load(); }
    catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  };

  const select = (setting: Setting) => { setSelected(setting); setEditor({ key: setting.key, value: JSON.stringify(setting.value, null, 2) }); };
  return <ui.Page title="全局设置" actions={<ui.Button onClick={() => void load()} loading={busy}>刷新</ui.Button>}>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title="租户设置">
        <ui.Table rowKey="key" rows={items as unknown as Array<Record<string, unknown>>} loading={busy} empty={<ui.EmptyState title="尚无设置" />} columns={[
          { key: "key", title: "键", render: (cell, row) => <ui.Button kind="text" onClick={() => select(row as unknown as Setting)}>{String(cell)}</ui.Button> },
          { key: "version", title: "版本", width: 90 },
          { key: "updatedAt", title: "更新时间", render: (cell) => formatTime(cell) },
        ]} />
      </ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={selected === undefined ? "新增设置" : `编辑 ${selected.key}`}>
        <ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
        <ui.Stack direction="row" gap="sm">
          <ui.Button kind="primary" onClick={() => void save()} loading={busy}>保存</ui.Button>
          {selected === undefined ? null : <ui.Button kind="danger" onClick={() => void remove()}>删除</ui.Button>}
          <ui.Button kind="secondary" onClick={() => { setSelected(undefined); setEditor({ value: "{}" }); }}>清空</ui.Button>
        </ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
  </ui.Page>;
}

function formatTime(value: unknown): string { return typeof value === "string" && value !== "" ? new Date(value).toLocaleString() : "-"; }
function message(cause: unknown): string { return cause instanceof Error ? cause.message : "平台设置请求失败"; }

export default {
  register(context: { addRoute(route: { path: string; component: typeof GlobalSettingsView }): void; addMenu(item: { id: string; title: string; route: string }): void }) {
    context.addRoute({ path: "/settings/global", component: GlobalSettingsView });
    context.addMenu({ id: "platform.global-settings", title: "全局设置", route: "/settings/global" });
  },
};
