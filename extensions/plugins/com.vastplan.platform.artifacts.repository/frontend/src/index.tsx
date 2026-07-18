import { useCallback, useEffect, useMemo, useState } from "react";
import { createBrowserPlatformAdminClient, type ArtifactRepositoryStatus, type PlatformAdminClient } from "@vastplan/platform-admin";
import { usePortalUI } from "@vastplan/portal-ui";

export function ArtifactRepositoryView({ client: supplied }: { client?: PlatformAdminClient } = {}) {
  const ui = usePortalUI();
  const client = useMemo(() => supplied ?? createBrowserPlatformAdminClient(), [supplied]);
  const [status, setStatus] = useState<ArtifactRepositoryStatus>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => { setBusy(true); try { setStatus(await client.artifactRepositoryStatus()); setError(undefined); } catch (cause) { setError(cause instanceof Error ? cause.message : "制品仓库状态请求失败"); } finally { setBusy(false); } }, [client]);
  useEffect(() => { void load(); }, [load]);
  return <ui.Page title="制品仓库" actions={<ui.Button onClick={() => void load()} loading={busy}>刷新</ui.Button>}>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    {busy && status === undefined ? <ui.Skeleton rows={4} /> : <ui.Panel title="运行状态"><ui.Descriptions columns={2} items={[
      { id: "ready", label: "服务状态", value: <ui.Status tone={status?.ready === true ? "success" : "error"}>{status?.ready === true ? "就绪" : "不可用"}</ui.Status> },
      { id: "listen", label: "监听地址", value: status?.listen ?? "-" },
      { id: "trust", label: "制品信任", value: "验签与安装授权由内核持有" },
      { id: "secrets", label: "仓库凭证", value: "不向 Portal 暴露" },
    ]} /></ui.Panel>}
  </ui.Page>;
}

export default {
  register(context: { addRoute(route: { path: string; component: typeof ArtifactRepositoryView }): void; addMenu(item: { id: string; title: string; route: string }): void }) {
    context.addRoute({ path: "/settings/artifacts", component: ArtifactRepositoryView });
    context.addMenu({ id: "platform.artifact-repository", title: "制品仓库", route: "/settings/artifacts" });
  },
};
