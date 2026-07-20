import { useCallback, useEffect, useState } from "react";
import { createBrowserPlatformAdminClient, type DatabaseConnection, type DatabasePoolPolicy, type PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, managementServicesFor, message as localizedMessage, usePortalMessages, usePortalUI, type FormSchema, type FrontendPluginContext } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";

const schema: FormSchema = {
  id: "platform-database-connection.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "providerId", "endpoint", "options"],
    properties: {
      name: { type: "string", title: "连接名称", minLength: 1, maxLength: 160 },
      providerId: { type: "string", title: "Provider", oneOf: [{ const: "postgresql", title: "PostgreSQL" }, { const: "mysql", title: "MySQL" }] },
      endpoint: { type: "string", title: "地址", minLength: 1 },
      database: { type: "string", title: "数据库" },
      options: { type: "object", title: "连接选项", additionalProperties: false, required: ["user"], properties: {
        user: { type: "string", title: "用户名", minLength: 1, maxLength: 128 },
        tlsMode: { type: "string", title: "TLS 模式", oneOf: [{ const: "verify-full", title: "完整校验（推荐）" }, { const: "disable", title: "关闭（仅受控测试环境）" }] },
        serverName: { type: "string", title: "TLS Server Name" }, connectTimeoutMs: { type: "integer", title: "连接超时（毫秒）", minimum: 100, maximum: 300000 },
        applicationName: { type: "string", title: "PostgreSQL Application Name" }, network: { type: "string", title: "MySQL 网络", oneOf: [{ const: "tcp", title: "TCP" }, { const: "unix", title: "Unix Socket" }] },
        readTimeoutMs: { type: "integer", title: "MySQL 读取超时（毫秒）", minimum: 0, maximum: 300000 }, writeTimeoutMs: { type: "integer", title: "MySQL 写入超时（毫秒）", minimum: 0, maximum: 300000 },
        rejectReadOnly: { type: "boolean", title: "MySQL 拒绝只读实例" },
      } },
      pool: { type: "object", title: "连接池", additionalProperties: false, properties: {
        minIdle: { type: "integer", title: "最小空闲连接", minimum: 0 }, maxIdle: { type: "integer", title: "最大空闲连接", minimum: 0 }, maxOpen: { type: "integer", title: "最大连接数", minimum: 1 },
        maxLifetimeMs: { type: "integer", title: "连接最长生命周期（毫秒）", minimum: 1000 }, maxIdleTimeMs: { type: "integer", title: "最长空闲时间（毫秒）", minimum: 1000 },
        acquireTimeoutMs: { type: "integer", title: "获取连接超时（毫秒）", minimum: 100 }, idlePoolTtlMs: { type: "integer", title: "空池回收时间（毫秒）", minimum: 1000 },
      } },
      credentialValue: { type: "string", title: "数据库凭证" },
    },
  },
  uiSchema: { credentialValue: { "ui:widget": "password", "ui:help": "新建时必填；编辑时留空会保留原凭证。明文只用于本次 TLS 请求。" } },
  localization: { "/properties/name/title": localizedMessage(namespace,"form.name","连接名称"), "/properties/providerId/title": localizedMessage(namespace,"form.provider","Provider"), "/properties/endpoint/title": localizedMessage(namespace,"form.endpoint","地址"), "/properties/database/title": localizedMessage(namespace,"form.database","数据库"), "/properties/credentialValue/title": localizedMessage(namespace,"form.credential","数据库凭证") },
  uiLocalization: { "/credentialValue/ui:help": localizedMessage(namespace,"form.credentialHelp","新建时必填；编辑时留空会保留原凭证。明文只用于本次 TLS 请求。") },
};

type Editor = { name?: string; providerId?: string; endpoint?: string; database?: string; options?: Record<string, unknown>; pool?: DatabasePoolPolicy; credentialValue?: string };

const emptyEditor = (): Editor => ({ providerId: "postgresql", options: { tlsMode: "verify-full", connectTimeoutMs: 10000 }, pool: { minIdle: 0, maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1800000, maxIdleTimeMs: 300000, acquireTimeoutMs: 5000, idlePoolTtlMs: 900000 } });

export function DatabaseConnectionsView({ client }: { client: PlatformAdminClient }) {
	const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  const [items, setItems] = useState<DatabaseConnection[]>([]);
  const [editor, setEditor] = useState<Editor>(emptyEditor);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setItems(await client.listDatabaseConnections()); setError(undefined); } catch (cause) { setError(errorMessage(cause,t("error.request","数据库连接请求失败"))); } finally { setBusy(false); } }, [client, t]);
  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (!editor.name || !editor.providerId || !editor.endpoint || !editor.options) return;
    setBusy(true);
    try {
      await client.putDatabaseConnection(editor.name, { providerId: editor.providerId, endpoint: editor.endpoint, options: editor.options, ...(editor.pool ? { pool: editor.pool } : {}), ...(editor.database ? { database: editor.database } : {}), ...(editor.credentialValue ? { credentialValue: editor.credentialValue } : {}) });
      setEditor(emptyEditor()); await load(); ui.notify({ title: t("notice.saved", "数据库连接已保存"), kind: "success" });
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
        { key: "name", title: t("column.name","名称"), render: (cell, row) => { const item = row as unknown as DatabaseConnection; return <ui.Button kind="text" onClick={() => setEditor({ name:item.name, providerId:item.providerId, endpoint:item.endpoint, options:item.options, pool:item.pool, ...(item.database ? {database:item.database} : {}) })}>{String(cell)}</ui.Button>; } },
        { key: "providerId", title: t("column.provider","Provider") }, { key: "endpoint", title: t("column.endpoint","地址") }, { key: "runtime", title: t("column.runtime","Runtime"), render: (cell) => cell === "ready" ? t("status.ready","已就绪") : t("status.pending","待发布") }, { key: "credential", title: t("column.credential","凭证"), render: (_cell, row) => (row as unknown as DatabaseConnection).credential?.managed ? `${t("status.managed","已托管")} v${(row as unknown as DatabaseConnection).credential.version}` : t("status.missing","未配置") },
        { key: "actions", title: t("column.actions","操作"), render: (_cell, row) => <ui.Stack direction="row" gap="sm"><ui.Button kind="secondary" onClick={() => void probe(row as unknown as DatabaseConnection)}>{t("action.probe","探测")}</ui.Button><ui.Button kind="danger" onClick={() => void remove(row as unknown as DatabaseConnection)}>{t("action.delete","删除")}</ui.Button></ui.Stack> },
      ]} /></ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={t("panel.editor","新增或更新连接")}><ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
        <ui.Stack direction="row" gap="sm"><ui.Button kind="primary" onClick={() => void save()} loading={busy}>{t("action.save","保存")}</ui.Button><ui.Button kind="secondary" onClick={() => setEditor(emptyEditor())}>{t("action.clear","清空")}</ui.Button></ui.Stack>
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
    "zh-CN":{"form.name":"连接名称","form.provider":"Provider","form.endpoint":"地址","form.database":"数据库","form.credential":"数据库凭证","form.credentialHelp":"新建时必填；编辑时留空会保留原凭证。明文只用于本次 TLS 请求。","error.request":"数据库连接请求失败","notice.saved":"数据库连接已保存","confirm.deleteTitle":"删除数据库连接","confirm.deleteContent":"确认删除 {name}？其托管凭证将一并退役。","status.ready":"已就绪","status.pending":"待发布","status.unavailable":"连接不可用","status.managed":"已托管","status.missing":"未配置","action.refresh":"刷新","panel.connections":"连接定义","empty.connections":"尚无数据库连接","column.name":"名称","column.provider":"Provider","column.endpoint":"地址","column.runtime":"Runtime","column.credential":"凭证","column.actions":"操作","action.probe":"探测","action.delete":"删除","panel.editor":"新增或更新连接","action.save":"保存","action.clear":"清空","page.title":"数据库连接","page.titleService":"数据库连接 · {service}","page.description":"在数据库插件内配置连接、Provider、连接池与托管凭证"},
    "en-US":{"form.name":"Connection name","form.provider":"Provider","form.endpoint":"Endpoint","form.database":"Database","form.credential":"Database credential","form.credentialHelp":"Required when creating. Leave blank when editing to retain the managed credential. Plaintext is used only for this TLS request.","error.request":"Database connection request failed","notice.saved":"Database connection saved","confirm.deleteTitle":"Delete database connection","confirm.deleteContent":"Delete {name} and retire its managed credential?","status.ready":"Ready","status.pending":"Pending publication","status.unavailable":"Connection unavailable","status.managed":"Managed","status.missing":"Missing","action.refresh":"Refresh","panel.connections":"Connection definitions","empty.connections":"No database connections","column.name":"Name","column.provider":"Provider","column.endpoint":"Endpoint","column.runtime":"Runtime","column.credential":"Credential","column.actions":"Actions","action.probe":"Probe","action.delete":"Delete","panel.editor":"Create or update connection","action.save":"Save","action.clear":"Clear","page.title":"Database connections","page.titleService":"Database connections · {service}","page.description":"Configure providers, pools and managed credentials in the database plugin"}
  }},
};
