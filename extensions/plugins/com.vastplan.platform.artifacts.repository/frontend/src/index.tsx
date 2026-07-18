import { useCallback, useEffect, useState } from "react";
import { createBrowserPlatformAdminClient, type ArtifactRepositoryStatus, type PlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, usePortalUI, type FrontendPluginContext } from "@vastplan/portal-ui";

export function ArtifactRepositoryView({ client }: { client: PlatformAdminClient }) {
	const ui = usePortalUI();
  const [status, setStatus] = useState<ArtifactRepositoryStatus>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setStatus(await client.artifactRepositoryStatus()); setError(undefined); } catch (cause) { setError(cause instanceof Error ? cause.message : "制品仓库状态请求失败"); } finally { setBusy(false); } }, [client]);
  useEffect(() => { void load(); }, [load]);
  return <ui.Stack gap="md"><ui.Stack direction="row" justify="end"><ui.Button onClick={() => void load()} loading={busy}>刷新</ui.Button></ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    {busy && status === undefined ? <ui.Skeleton rows={4} /> : <ui.Panel title="运行状态"><ui.Descriptions columns={2} items={[
      { id: "ready", label: "服务状态", value: <ui.Status tone={status?.ready === true ? "success" : "error"}>{status?.ready === true ? "就绪" : "不可用"}</ui.Status> },
      { id: "listen", label: "监听地址", value: status?.listen ?? "-" },
      { id: "trust", label: "制品信任", value: "验签与安装授权由内核持有" },
      { id: "secrets", label: "仓库凭证", value: "不向 Portal 暴露" },
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
			const label = services.length === 1 ? "制品仓库" : `制品仓库 · ${service.label ?? service.id}`;
			context.addPage({ id: `platform.artifact-repository.${service.id}`, path: `/settings/artifacts${suffix}`, title: label, description: "查看可信制品服务运行状态", navigation: { id: `platform.artifact-repository.${service.id}`, label, zone: "settings", order: 50 }, slots: [{ id: "body", slot: "page.body.main", component: () => <ArtifactRepositoryView client={client} /> }] });
		}
	},
};
