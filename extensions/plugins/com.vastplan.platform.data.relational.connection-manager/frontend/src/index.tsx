import { useCallback, useEffect, useMemo, useState } from "react";
import { createBrowserPlatformAdminClient, type DatabaseConnection, type PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, usePortalUI, type FormSchema } from "@vastplan/portal-ui";

const schema: FormSchema = {
  id: "platform-database-connection.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "driver", "endpoint", "credential"],
    properties: {
      name: { type: "string", title: "连接名称", minLength: 1, maxLength: 160 },
      driver: { type: "string", title: "驱动", minLength: 1 },
      endpoint: { type: "string", title: "地址", minLength: 1 },
      database: { type: "string", title: "数据库" },
      credential: { type: "string", title: "CredentialRef", minLength: 1 },
    },
  },
  uiSchema: { credential: { "ui:help": "仅填写凭证名称；连接定义不会保存密码。" } },
};

type Editor = Partial<DatabaseConnection>;

export function DatabaseConnectionsView({ client: supplied }: { client?: PlatformAdminClient } = {}) {
  const ui = usePortalUI();
  const client = useMemo(() => supplied ?? createBrowserPlatformAdminClient(), [supplied]);
  const [items, setItems] = useState<DatabaseConnection[]>([]);
  const [editor, setEditor] = useState<Editor>({ driver: "postgres" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setItems(await client.listDatabaseConnections()); setError(undefined); } catch (cause) { setError(message(cause)); } finally { setBusy(false); } }, [client]);
  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (!editor.name || !editor.driver || !editor.endpoint || !editor.credential) return;
    setBusy(true);
    try {
      await client.putDatabaseConnection(editor.name, { driver: editor.driver, endpoint: editor.endpoint, ...(editor.database ? { database: editor.database } : {}), credential: editor.credential });
      setEditor({ driver: "postgres" }); await load(); ui.notify({ title: "数据库连接已保存", kind: "success" });
    } catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  };

  const remove = async (item: DatabaseConnection) => {
    if (!await ui.confirm({ title: "删除数据库连接", content: `确认删除 ${item.name}？凭证本身不会被删除。` })) return;
    setBusy(true); try { await client.deleteDatabaseConnection(item.name); await load(); } catch (cause) { setError(message(cause)); } finally { setBusy(false); }
  };
  const probe = async (item: DatabaseConnection) => {
    setBusy(true);
    try { const result = await client.probeDatabaseConnection(item.name); ui.notify({ title: result.ready ? "连接正常" : "连接不可用", content: result.message, kind: result.ready ? "success" : "error" }); }
    catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  };

  return <ui.Page title="数据库连接" actions={<ui.Button onClick={() => void load()} loading={busy}>刷新</ui.Button>}>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title="连接定义"><ui.Table rowKey="name" rows={items as unknown as Array<Record<string, unknown>>} loading={busy} empty={<ui.EmptyState title="尚无数据库连接" />} columns={[
        { key: "name", title: "名称", render: (cell, row) => <ui.Button kind="text" onClick={() => setEditor(row as Editor)}>{String(cell)}</ui.Button> },
        { key: "driver", title: "驱动" }, { key: "endpoint", title: "地址" }, { key: "credential", title: "凭证引用" },
        { key: "actions", title: "操作", render: (_cell, row) => <ui.Stack direction="row" gap="sm"><ui.Button kind="secondary" onClick={() => void probe(row as unknown as DatabaseConnection)}>探测</ui.Button><ui.Button kind="danger" onClick={() => void remove(row as unknown as DatabaseConnection)}>删除</ui.Button></ui.Stack> },
      ]} /></ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title="新增或更新连接"><ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
        <ui.Stack direction="row" gap="sm"><ui.Button kind="primary" onClick={() => void save()} loading={busy}>保存</ui.Button><ui.Button kind="secondary" onClick={() => setEditor({ driver: "postgres" })}>清空</ui.Button></ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
  </ui.Page>;
}

function message(cause: unknown): string { return cause instanceof Error ? cause.message : "数据库连接请求失败"; }
export default {
  register(context: { addRoute(route: { path: string; component: typeof DatabaseConnectionsView }): void; addMenu(item: { id: string; title: string; route: string }): void }) {
    context.addRoute({ path: "/settings/databases", component: DatabaseConnectionsView });
    context.addMenu({ id: "platform.database-connections", title: "数据库连接", route: "/settings/databases" });
  },
};
