import { useCallback, useEffect, useState } from "react";
import { createBrowserPlatformAdminClient, type PlatformAdminClient, type Setting } from "@vastplan/platform-admin";
import { jsonSchemaDialect, managementServicesFor, message, usePortalI18n, usePortalUI, type FormSchema, type FrontendPluginContext } from "@vastplan/portal-ui";

const namespace = "com.vastplan.platform.configuration.global-settings";

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
  localization: { "/properties/key/title": message(namespace, "form.key", "设置键"), "/properties/value/title": message(namespace, "form.value", "JSON 值") },
  uiLocalization: { "/value/ui:help": message(namespace, "form.valueHelp", "保存前会校验为有效 JSON；禁止保存密码和令牌。") },
};

type Editor = { key?: string; value?: string };

export function GlobalSettingsView({ client }: { client: PlatformAdminClient }) {
	const ui = usePortalUI();
  const i18n = usePortalI18n();
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => i18n.text(message(namespace, key, fallback, values));
  const [items, setItems] = useState<Setting[]>([]);
  const [editor, setEditor] = useState<Editor>({ value: "{}" });
  const [selected, setSelected] = useState<Setting>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setBusy(true);
    try { setItems(await client.listSettings()); setError(undefined); }
    catch (cause) { setError(errorMessage(cause, t("error.request", "平台设置请求失败"))); }
    finally { setBusy(false); }
  }, [client]);
  useEffect(() => { void load(); }, [load]);

  const save = async () => {
    if (editor.key === undefined || editor.value === undefined) return;
    let value: unknown;
    try { value = JSON.parse(editor.value); } catch { setError(t("error.invalidJSON", "设置值不是有效 JSON")); return; }
    setBusy(true);
    try {
      await client.putSetting(editor.key, value, selected?.version);
      ui.notify({ title: t("notice.saved", "设置已保存"), kind: "success" });
      setSelected(undefined); setEditor({ value: "{}" }); await load();
    } catch (cause) { setError(errorMessage(cause, t("error.request", "平台设置请求失败"))); }
    finally { setBusy(false); }
  };

  const remove = async () => {
    if (selected === undefined || !await ui.confirm({ title: t("confirm.deleteTitle", "删除设置"), content: t("confirm.deleteContent", "确认删除 {key}？版本不匹配时系统会拒绝。", { key: selected.key }) })) return;
    setBusy(true);
    try { await client.deleteSetting(selected.key, selected.version); setSelected(undefined); setEditor({ value: "{}" }); await load(); }
    catch (cause) { setError(errorMessage(cause, t("error.request", "平台设置请求失败"))); }
    finally { setBusy(false); }
  };

  const select = (setting: Setting) => { setSelected(setting); setEditor({ key: setting.key, value: JSON.stringify(setting.value, null, 2) }); };
  return <ui.Stack gap="md"><ui.Stack direction="row" justify="between" align="center"><ui.Stack direction="row" gap="sm" align="center"><span>{t("locale.label","界面语言")}</span>{i18n.supportedLocales.map((locale) => <ui.Button key={locale} kind={locale === i18n.locale ? "primary" : "secondary"} disabled={locale === i18n.locale} onClick={() => i18n.setLocale(locale)}>{locale}</ui.Button>)}</ui.Stack><ui.Button onClick={() => void load()} loading={busy}>{t("action.refresh", "刷新")}</ui.Button></ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title={t("panel.settings", "租户设置")}>
        <ui.Table rowKey="key" rows={items as unknown as Array<Record<string, unknown>>} loading={busy} empty={<ui.EmptyState title={t("empty.settings", "尚无设置")} />} columns={[
          { key: "key", title: t("column.key", "键"), render: (cell, row) => <ui.Button kind="text" onClick={() => select(row as unknown as Setting)}>{String(cell)}</ui.Button> },
          { key: "version", title: t("column.version", "版本"), width: 90 },
          { key: "updatedAt", title: t("column.updatedAt", "更新时间"), render: (cell) => formatTime(cell, i18n.formatDate) },
        ]} />
      </ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={selected === undefined ? t("panel.create", "新增设置") : t("panel.edit", "编辑 {key}", { key: selected.key })}>
        <ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
        <ui.Stack direction="row" gap="sm">
          <ui.Button kind="primary" onClick={() => void save()} loading={busy}>{t("action.save", "保存")}</ui.Button>
          {selected === undefined ? null : <ui.Button kind="danger" onClick={() => void remove()}>{t("action.delete", "删除")}</ui.Button>}
          <ui.Button kind="secondary" onClick={() => { setSelected(undefined); setEditor({ value: "{}" }); }}>{t("action.clear", "清空")}</ui.Button>
        </ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
  </ui.Stack>;
}

function formatTime(value: unknown, format: (value: string) => string): string { return typeof value === "string" && value !== "" ? format(value) : "-"; }
function errorMessage(cause: unknown, fallback: string): string { return cause instanceof Error ? cause.message : fallback; }

export default {
	register(context: FrontendPluginContext) {
		const services = managementServicesFor(context.portal, "platform.settings");
		if (services.length === 0) throw new Error("Portal 未绑定 platform.settings 服务");
		for (const service of services) {
			const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
			const suffix = services.length === 1 ? "" : `/${service.id}`;
			const label = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "全局设置" : "全局设置 · {service}", { service: service.label ?? service.id });
			context.addPage({ id: `platform.global-settings.${service.id}`, path: `/settings/global${suffix}`, title: label, description: context.i18n.message("page.description", "管理平台级非敏感配置"), navigation: { id: `platform.global-settings.${service.id}`, label, zone: "settings", order: 20 }, slots: [{ id: "body", slot: "page.body.main", component: () => <GlobalSettingsView client={client} /> }] });
		}
	},
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "form.key":"设置键","form.value":"JSON 值","form.valueHelp":"保存前会校验为有效 JSON；禁止保存密码和令牌。","error.request":"平台设置请求失败","error.invalidJSON":"设置值不是有效 JSON","notice.saved":"设置已保存","confirm.deleteTitle":"删除设置","confirm.deleteContent":"确认删除 {key}？版本不匹配时系统会拒绝。","locale.label":"界面语言","action.refresh":"刷新","action.save":"保存","action.delete":"删除","action.clear":"清空","panel.settings":"租户设置","empty.settings":"尚无设置","column.key":"键","column.version":"版本","column.updatedAt":"更新时间","panel.create":"新增设置","panel.edit":"编辑 {key}","page.title":"全局设置","page.titleService":"全局设置 · {service}","page.description":"管理平台级非敏感配置" },
    "en-US": { "form.key":"Setting key","form.value":"JSON value","form.valueHelp":"The value must be valid JSON. Passwords and tokens are not allowed.","error.request":"Platform settings request failed","error.invalidJSON":"The setting value is not valid JSON","notice.saved":"Setting saved","confirm.deleteTitle":"Delete setting","confirm.deleteContent":"Delete {key}? The request will be rejected if the version does not match.","locale.label":"Interface language","action.refresh":"Refresh","action.save":"Save","action.delete":"Delete","action.clear":"Clear","panel.settings":"Tenant settings","empty.settings":"No settings","column.key":"Key","column.version":"Version","column.updatedAt":"Updated","panel.create":"Create setting","panel.edit":"Edit {key}","page.title":"Global settings","page.titleService":"Global settings · {service}","page.description":"Manage non-sensitive platform configuration" }
  } },
};
