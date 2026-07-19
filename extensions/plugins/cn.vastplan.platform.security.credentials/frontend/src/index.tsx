import { useCallback, useEffect, useState } from "react";
import { createBrowserPlatformAdminClient, type CredentialMetadata, type PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, managementServicesFor, message as localizedMessage, usePortalI18n, usePortalMessages, usePortalUI, type FormSchema, type FrontendPluginContext } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.platform.security.credentials";

const schema: FormSchema = {
  id: "platform-credential.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "value"],
    properties: {
      name: { type: "string", title: "凭证名称", minLength: 1, maxLength: 160 },
      value: { type: "string", title: "凭证明文", minLength: 1 },
    },
  },
  uiSchema: { value: { "ui:widget": "password", "ui:help": "明文仅用于本次 TLS 请求，不会被 API 返回、写入日志或保存在浏览器状态之外。" } },
  localization: { "/properties/name/title": localizedMessage(namespace,"form.name","凭证名称"), "/properties/value/title": localizedMessage(namespace,"form.value","凭证明文") },
  uiLocalization: { "/value/ui:help": localizedMessage(namespace,"form.valueHelp","明文仅用于本次 TLS 请求，不会被 API 返回、写入日志或保存在浏览器状态之外。") },
};

type Editor = { name?: string; value?: string };

export function CredentialsView({ client }: { client: PlatformAdminClient }) {
	const ui = usePortalUI();
  const i18n = usePortalI18n();
  const t = usePortalMessages(namespace);
  const [items, setItems] = useState<CredentialMetadata[]>([]);
  const [editor, setEditor] = useState<Editor>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setItems(await client.listCredentials()); setError(undefined); } catch (cause) { setError(message(cause)); } finally { setBusy(false); } }, [client]);
  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (!editor.name || !editor.value) return;
    setBusy(true);
    try {
      await client.putCredential(editor.name, editor.value);
      setEditor({});
      ui.notify({ title: t("notice.saved","凭证已安全保存"), kind: "success" });
      await load();
    } catch (cause) { setError(message(cause)); }
    finally { setEditor((current) => ({ name: current.name })); setBusy(false); }
  };

  const action = async (item: CredentialMetadata, kind: "rotate" | "revoke") => {
    const title = kind === "rotate" ? t("confirm.rotate","轮换包裹密钥") : t("confirm.revoke","撤销凭证");
    if (!await ui.confirm({ title, content: t("confirm.content","{action} {name}？",{action:title,name:item.name}) })) return;
    setBusy(true);
    try { kind === "rotate" ? await client.rotateCredential(item.name) : await client.revokeCredential(item.name); await load(); }
    catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  };

  return <ui.Stack gap="md"><ui.Stack direction="row" justify="end"><ui.Button onClick={() => void load()} loading={busy}>{t("action.refresh","刷新")}</ui.Button></ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Panel title={t("panel.save","保存或替换凭证")}><ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
      <ui.Button kind="primary" onClick={() => void save()} loading={busy}>{t("action.save","安全保存")}</ui.Button>
    </ui.Panel>
    <ui.Panel title={t("panel.metadata","凭证元数据")}>
      <ui.Table rowKey="name" rows={items as unknown as Array<Record<string, unknown>>} loading={busy} empty={<ui.EmptyState title={t("empty.title","尚无凭证")} description={t("empty.description","列表永远不会包含明文或密文。")} />} columns={[
        { key: "name", title: t("column.name","名称") },
        { key: "version", title: t("column.version","版本"), width: 90 },
        { key: "keyVersion", title: t("column.keyVersion","包裹密钥") },
        { key: "revoked", title: t("column.status","状态"), render: (cell) => <ui.Status tone={cell === true ? "error" : "success"}>{cell === true ? t("status.revoked","已撤销") : t("status.available","可用")}</ui.Status> },
        { key: "updatedAt", title: t("column.updatedAt","更新时间"), render: (cell) => formatTime(cell, i18n.formatDate) },
        { key: "actions", title: t("column.actions","操作"), render: (_cell, row) => <ui.Stack direction="row" gap="sm"><ui.Button kind="secondary" onClick={() => void action(row as unknown as CredentialMetadata, "rotate")}>{t("action.rotate","轮换")}</ui.Button><ui.Button kind="danger" onClick={() => void action(row as unknown as CredentialMetadata, "revoke")}>{t("action.revoke","撤销")}</ui.Button></ui.Stack> },
      ]} />
    </ui.Panel>
  </ui.Stack>;
}

function formatTime(value: unknown, format: (value: string) => string): string { return typeof value === "string" && value !== "" ? format(value) : "-"; }
function message(cause: unknown): string { return cause instanceof Error ? cause.message : "凭证请求失败"; }

export default {
	register(context: FrontendPluginContext) {
		const services = managementServicesFor(context.portal, "platform.credentials");
		if (services.length === 0) throw new Error("Portal 未绑定 platform.credentials 服务");
		for (const service of services) {
			const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
			const suffix = services.length === 1 ? "" : `/${service.id}`;
			const label = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "凭证引用" : "凭证引用 · {service}", { service: service.label ?? service.id });
			context.addPage({ id: `platform.credentials.${service.id}`, path: `/settings/credentials${suffix}`, title: label, description: context.i18n.message("page.description","管理不返回明文的凭证元数据"), navigation: { id: `platform.credentials.${service.id}`, label, zone: "settings", order: 30 }, slots: [{ id: "body", slot: "page.body.main", component: () => <CredentialsView client={client} /> }] });
		}
	},
  localization:{defaultLocale:"zh-CN",messages:{
    "zh-CN":{"form.name":"凭证名称","form.value":"凭证明文","form.valueHelp":"明文仅用于本次 TLS 请求，不会被 API 返回、写入日志或保存在浏览器状态之外。","notice.saved":"凭证已安全保存","confirm.rotate":"轮换包裹密钥","confirm.revoke":"撤销凭证","confirm.content":"{action} {name}？","action.refresh":"刷新","panel.save":"保存或替换凭证","action.save":"安全保存","panel.metadata":"凭证元数据","empty.title":"尚无凭证","empty.description":"列表永远不会包含明文或密文。","column.name":"名称","column.version":"版本","column.keyVersion":"包裹密钥","column.status":"状态","status.revoked":"已撤销","status.available":"可用","column.updatedAt":"更新时间","column.actions":"操作","action.rotate":"轮换","action.revoke":"撤销","page.title":"凭证引用","page.titleService":"凭证引用 · {service}","page.description":"管理不返回明文的凭证元数据"},
    "en-US":{"form.name":"Credential name","form.value":"Credential plaintext","form.valueHelp":"Plaintext is used only for this TLS request. It is never returned by the API, logged, or retained in browser state.","notice.saved":"Credential saved securely","confirm.rotate":"Rotate wrapping key","confirm.revoke":"Revoke credential","confirm.content":"{action} {name}?","action.refresh":"Refresh","panel.save":"Save or replace credential","action.save":"Save securely","panel.metadata":"Credential metadata","empty.title":"No credentials","empty.description":"The list never contains plaintext or ciphertext.","column.name":"Name","column.version":"Version","column.keyVersion":"Wrapping key","column.status":"Status","status.revoked":"Revoked","status.available":"Available","column.updatedAt":"Updated","column.actions":"Actions","action.rotate":"Rotate","action.revoke":"Revoke","page.title":"Credential references","page.titleService":"Credential references · {service}","page.description":"Manage credential metadata without exposing plaintext"}
  }},
};
