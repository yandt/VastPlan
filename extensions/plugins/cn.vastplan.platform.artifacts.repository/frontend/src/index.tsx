import { useCallback, useEffect, useState } from "react";
import { createBrowserPlatformAdminClient, type ArtifactRepositoryStatus, type PlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, message, usePortalI18n, usePortalUI, type FrontendPluginContext } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.platform.artifacts.repository";

export function ArtifactRepositoryView({ client }: { client: PlatformAdminClient }) {
	const ui = usePortalUI();
  const i18n = usePortalI18n();
  const t = (key: string, fallback: string) => i18n.text(message(namespace, key, fallback));
  const [status, setStatus] = useState<ArtifactRepositoryStatus>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setStatus(await client.artifactRepositoryStatus()); setError(undefined); } catch (cause) { setError(cause instanceof Error ? cause.message : t("error.request", "制品仓库状态请求失败")); } finally { setBusy(false); } }, [client, i18n.locale]);
  useEffect(() => { void load(); }, [load]);
  return <ui.Stack gap="md"><ui.Stack direction="row" justify="end"><ui.Button onClick={() => void load()} loading={busy}>{t("action.refresh", "刷新")}</ui.Button></ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    {busy && status === undefined ? <ui.Skeleton rows={4} /> : <ui.Panel title={t("panel.status", "运行状态")}><ui.Descriptions columns={2} items={[
      { id: "ready", label: t("field.status", "服务状态"), value: <ui.Status tone={status?.ready === true ? "success" : "error"}>{status?.ready === true ? t("status.ready", "就绪") : t("status.unavailable", "不可用")}</ui.Status> },
      { id: "listen", label: t("field.listen", "监听地址"), value: status?.listen ?? "-" },
      { id: "storageProvider", label: t("field.storageProvider", "存储 Provider"), value: status?.storageProvider ?? "-" },
      { id: "trust", label: t("field.trust", "制品信任"), value: t("value.trust", "验签与安装授权由内核持有") },
      { id: "secrets", label: t("field.credentials", "仓库凭证"), value: t("value.credentials", "不向 Portal 暴露") },
    ]} /></ui.Panel>}
  </ui.Stack>;
}

export default {
	register(context: FrontendPluginContext) {
		const services = managementServicesFor(context.portal, "platform.artifacts.repository");
		if (services.length === 0) throw new Error("Portal 未绑定 platform.artifacts.repository 服务");
		for (const service of services) {
			const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
			const suffix = services.length === 1 ? "" : `/${service.id}`;
			const label = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "制品仓库" : "制品仓库 · {service}", { service: service.label ?? service.id });
			context.addPage({ id: `platform.artifact-repository.${service.id}`, path: `/settings/artifacts${suffix}`, title: label, description: context.i18n.message("page.description", "查看可信制品服务运行状态"), navigation: { id: `platform.artifact-repository.${service.id}`, label, zone: "settings", order: 50 }, slots: [{ id: "body", slot: "page.body.main", component: () => <ArtifactRepositoryView client={client} /> }] });
		}
	},
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "error.request":"制品仓库状态请求失败","action.refresh":"刷新","panel.status":"运行状态","field.status":"服务状态","status.ready":"就绪","status.unavailable":"不可用","field.listen":"监听地址","field.storageProvider":"存储 Provider","field.trust":"制品信任","value.trust":"验签与安装授权由内核持有","field.credentials":"仓库凭证","value.credentials":"不向 Portal 暴露","page.title":"制品仓库","page.titleService":"制品仓库 · {service}","page.description":"查看可信制品服务运行状态" },
    "en-US": { "error.request":"Artifact repository status request failed","action.refresh":"Refresh","panel.status":"Runtime status","field.status":"Service status","status.ready":"Ready","status.unavailable":"Unavailable","field.listen":"Listen address","field.storageProvider":"Storage provider","field.trust":"Artifact trust","value.trust":"Signature verification and installation authorization are held by the kernel","field.credentials":"Repository credentials","value.credentials":"Not exposed to the Portal","page.title":"Artifact repository","page.titleService":"Artifact repository · {service}","page.description":"View trusted artifact service runtime status" }
  } },
};
