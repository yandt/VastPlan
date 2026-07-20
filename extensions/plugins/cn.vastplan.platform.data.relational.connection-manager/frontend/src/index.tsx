import { useCallback, useEffect, useState } from "react";
import { createBrowserPlatformAdminClient, type DatabaseConnection, type PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, managementServicesFor, message as localizedMessage, usePortalMessages, usePortalUI, type FormSchema, type FrontendPluginContext } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";

const schema: FormSchema = {
  id: "platform-database-connection.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "driver", "endpoint"],
    properties: {
      name: { type: "string", title: "连接名称", minLength: 1, maxLength: 160 },
      driver: { type: "string", title: "驱动", minLength: 1 },
      endpoint: { type: "string", title: "地址", minLength: 1 },
      database: { type: "string", title: "数据库" },
      credentialValue: { type: "string", title: "数据库凭证" },
    },
  },
  uiSchema: { credentialValue: { "ui:widget": "password", "ui:help": "新建时必填；编辑时留空会保留原凭证。明文只用于本次 TLS 请求。" } },
  localization: { "/properties/name/title": localizedMessage(namespace,"form.name","连接名称"), "/properties/driver/title": localizedMessage(namespace,"form.driver","驱动"), "/properties/endpoint/title": localizedMessage(namespace,"form.endpoint","地址"), "/properties/database/title": localizedMessage(namespace,"form.database","数据库"), "/properties/credentialValue/title": localizedMessage(namespace,"form.credential","数据库凭证") },
  uiLocalization: { "/credentialValue/ui:help": localizedMessage(namespace,"form.credentialHelp","新建时必填；编辑时留空会保留原凭证。明文只用于本次 TLS 请求。") },
};

type Editor = { name?: string; driver?: string; endpoint?: string; database?: string; credentialValue?: string };

export function DatabaseConnectionsView({ client }: { client: PlatformAdminClient }) {
	const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  const [items, setItems] = useState<DatabaseConnection[]>([]);
  const [editor, setEditor] = useState<Editor>({ driver: "postgres" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setItems(await client.listDatabaseConnections()); setError(undefined); } catch (cause) { setError(errorMessage(cause,t("error.request","数据库连接请求失败"))); } finally { setBusy(false); } }, [client, t]);
  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (!editor.name || !editor.driver || !editor.endpoint) return;
    setBusy(true);
    try {
      await client.putDatabaseConnection(editor.name, { driver: editor.driver, endpoint: editor.endpoint, ...(editor.database ? { database: editor.database } : {}), ...(editor.credentialValue ? { credentialValue: editor.credentialValue } : {}) });
      setEditor({ driver: "postgres" }); await load(); ui.notify({ title: t("notice.saved", "数据库连接已保存"), kind: "success" });
    } catch (cause) { setError(errorMessage(cause,t("error.request","数据库连接请求失败"))); }
    finally { setBusy(false); }
  };

  const remove = async (item: DatabaseConnection) => {
    if (!await ui.confirm({ title: t("confirm.deleteTitle","删除数据库连接"), content: t("confirm.deleteContent","确认删除 {name}？其托管凭证将一并退役。",{name:item.name}) })) return;
    setBusy(true); try { await client.deleteDatabaseConnection(item.name); await load(); } catch (cause) { setError(errorMessage(cause,t("error.request","数据库连接请求失败"))); } finally { setBusy(false); }
  };
  const probe = async (item: DatabaseConnection) => {
    setBusy(true);
    try { const result = await client.probeDatabaseConnection(item.name); ui.notify({ title: result.ready ? t("status.ready","连接正常") : t("status.unavailable","连接不可用"), content: result.message, kind: result.ready ? "success" : "error" }); }
    catch (cause) { setError(errorMessage(cause,t("error.request","数据库连接请求失败"))); }
    finally { setBusy(false); }
  };

  return <ui.Stack gap="md"><ui.Stack direction="row" justify="end"><ui.Button onClick={() => void load()} loading={busy}>{t("action.refresh","刷新")}</ui.Button></ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title={t("panel.connections","连接定义")}><ui.Table rowKey="name" rows={items as unknown as Array<Record<string, unknown>>} loading={busy} empty={<ui.EmptyState title={t("empty.connections","尚无数据库连接")} />} columns={[
        { key: "name", title: t("column.name","名称"), render: (cell, row) => { const item = row as unknown as DatabaseConnection; return <ui.Button kind="text" onClick={() => setEditor({ name:item.name, driver:item.driver, endpoint:item.endpoint, ...(item.database ? {database:item.database} : {}) })}>{String(cell)}</ui.Button>; } },
        { key: "driver", title: t("column.driver","驱动") }, { key: "endpoint", title: t("column.endpoint","地址") }, { key: "credential", title: t("column.credential","凭证"), render: (_cell, row) => (row as unknown as DatabaseConnection).credential?.managed ? `${t("status.managed","已托管")} v${(row as unknown as DatabaseConnection).credential.version}` : t("status.missing","未配置") },
        { key: "actions", title: t("column.actions","操作"), render: (_cell, row) => <ui.Stack direction="row" gap="sm"><ui.Button kind="secondary" onClick={() => void probe(row as unknown as DatabaseConnection)}>{t("action.probe","探测")}</ui.Button><ui.Button kind="danger" onClick={() => void remove(row as unknown as DatabaseConnection)}>{t("action.delete","删除")}</ui.Button></ui.Stack> },
      ]} /></ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={t("panel.editor","新增或更新连接")}><ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
        <ui.Stack direction="row" gap="sm"><ui.Button kind="primary" onClick={() => void save()} loading={busy}>{t("action.save","保存")}</ui.Button><ui.Button kind="secondary" onClick={() => setEditor({ driver: "postgres" })}>{t("action.clear","清空")}</ui.Button></ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
  </ui.Stack>;
}

function errorMessage(cause: unknown, fallback: string): string { return cause instanceof Error ? cause.message : fallback; }
export default {
	register(context: FrontendPluginContext) {
		const services = managementServicesFor(context.portal, "platform.database");
		if (services.length === 0) throw new Error("Portal 未绑定 platform.database 服务");
		for (const service of services) {
			const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
			const suffix = services.length === 1 ? "" : `/${service.id}`;
			const label = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "数据库连接" : "数据库连接 · {service}", { service: service.label ?? service.id });
			context.addPage({ id: `platform.database-connections.${service.id}`, path: `/settings/databases${suffix}`, title: label, description: context.i18n.message("page.description","在数据库插件内配置连接与托管凭证"), navigation: { id: `platform.database-connections.${service.id}`, label, zone: "settings", order: 40 }, slots: [{ id: "body", slot: "page.body.main", component: () => <DatabaseConnectionsView client={client} /> }] });
		}
	},
  localization: { defaultLocale:"zh-CN", messages:{
    "zh-CN":{"form.name":"连接名称","form.driver":"驱动","form.endpoint":"地址","form.database":"数据库","form.credential":"数据库凭证","form.credentialHelp":"新建时必填；编辑时留空会保留原凭证。明文只用于本次 TLS 请求。","error.request":"数据库连接请求失败","notice.saved":"数据库连接已保存","confirm.deleteTitle":"删除数据库连接","confirm.deleteContent":"确认删除 {name}？其托管凭证将一并退役。","status.ready":"连接正常","status.unavailable":"连接不可用","status.managed":"已托管","status.missing":"未配置","action.refresh":"刷新","panel.connections":"连接定义","empty.connections":"尚无数据库连接","column.name":"名称","column.driver":"驱动","column.endpoint":"地址","column.credential":"凭证","column.actions":"操作","action.probe":"探测","action.delete":"删除","panel.editor":"新增或更新连接","action.save":"保存","action.clear":"清空","page.title":"数据库连接","page.titleService":"数据库连接 · {service}","page.description":"在数据库插件内配置连接与托管凭证"},
    "en-US":{"form.name":"Connection name","form.driver":"Driver","form.endpoint":"Endpoint","form.database":"Database","form.credential":"Database credential","form.credentialHelp":"Required when creating. Leave blank when editing to retain the managed credential. Plaintext is used only for this TLS request.","error.request":"Database connection request failed","notice.saved":"Database connection saved","confirm.deleteTitle":"Delete database connection","confirm.deleteContent":"Delete {name} and retire its managed credential?","status.ready":"Connection ready","status.unavailable":"Connection unavailable","status.managed":"Managed","status.missing":"Missing","action.refresh":"Refresh","panel.connections":"Connection definitions","empty.connections":"No database connections","column.name":"Name","column.driver":"Driver","column.endpoint":"Endpoint","column.credential":"Credential","column.actions":"Actions","action.probe":"Probe","action.delete":"Delete","panel.editor":"Create or update connection","action.save":"Save","action.clear":"Clear","page.title":"Database connections","page.titleService":"Database connections · {service}","page.description":"Configure connections and managed credentials in the database plugin"}
  }},
};
