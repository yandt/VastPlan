import { useCallback, useEffect, useMemo, useState } from "react";
import { createBrowserPlatformAdminClient, type CredentialMetadata, type PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, usePortalUI, type FormSchema, type FrontendPluginContext } from "@vastplan/portal-ui";

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
};

type Editor = { name?: string; value?: string };

export function CredentialsView({ client: supplied }: { client?: PlatformAdminClient } = {}) {
  const ui = usePortalUI();
  const client = useMemo(() => supplied ?? createBrowserPlatformAdminClient(), [supplied]);
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
      ui.notify({ title: "凭证已安全保存", kind: "success" });
      await load();
    } catch (cause) { setError(message(cause)); }
    finally { setEditor((current) => ({ name: current.name })); setBusy(false); }
  };

  const action = async (item: CredentialMetadata, kind: "rotate" | "revoke") => {
    const title = kind === "rotate" ? "轮换包裹密钥" : "撤销凭证";
    if (!await ui.confirm({ title, content: `${title} ${item.name}？` })) return;
    setBusy(true);
    try { kind === "rotate" ? await client.rotateCredential(item.name) : await client.revokeCredential(item.name); await load(); }
    catch (cause) { setError(message(cause)); }
    finally { setBusy(false); }
  };

  return <ui.Stack gap="md"><ui.Stack direction="row" justify="end"><ui.Button onClick={() => void load()} loading={busy}>刷新</ui.Button></ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Panel title="保存或替换凭证"><ui.FormRenderer schema={schema} value={editor} onChange={setEditor} submitting={busy} />
      <ui.Button kind="primary" onClick={() => void save()} loading={busy}>安全保存</ui.Button>
    </ui.Panel>
    <ui.Panel title="凭证元数据">
      <ui.Table rowKey="name" rows={items as unknown as Array<Record<string, unknown>>} loading={busy} empty={<ui.EmptyState title="尚无凭证" description="列表永远不会包含明文或密文。" />} columns={[
        { key: "name", title: "名称" },
        { key: "version", title: "版本", width: 90 },
        { key: "keyVersion", title: "包裹密钥" },
        { key: "revoked", title: "状态", render: (cell) => <ui.Status tone={cell === true ? "error" : "success"}>{cell === true ? "已撤销" : "可用"}</ui.Status> },
        { key: "updatedAt", title: "更新时间", render: (cell) => formatTime(cell) },
        { key: "actions", title: "操作", render: (_cell, row) => <ui.Stack direction="row" gap="sm"><ui.Button kind="secondary" onClick={() => void action(row as unknown as CredentialMetadata, "rotate")}>轮换</ui.Button><ui.Button kind="danger" onClick={() => void action(row as unknown as CredentialMetadata, "revoke")}>撤销</ui.Button></ui.Stack> },
      ]} />
    </ui.Panel>
  </ui.Stack>;
}

function formatTime(value: unknown): string { return typeof value === "string" && value !== "" ? new Date(value).toLocaleString() : "-"; }
function message(cause: unknown): string { return cause instanceof Error ? cause.message : "凭证请求失败"; }

export default {
  register(context: FrontendPluginContext) {
    context.addPage({ id: "platform.credentials", path: "/settings/credentials", title: "凭证引用", description: "管理不返回明文的凭证元数据", navigation: { id: "platform.credentials", label: "凭证引用", zone: "settings", order: 30 }, slots: [{ id: "body", slot: "page.body.main", component: CredentialsView }] });
  },
};
