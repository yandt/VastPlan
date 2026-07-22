import type {
  AuthenticationProviderManagementState,
  PlatformAdminClient,
} from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  message,
  type CollectionPageDefinition,
  type CollectionQuery,
} from "@vastplan/workbench-sdk";
import { providerForms } from "./forms.js";
import { namespace, type ProviderRow } from "./model.js";

export function createAuthenticationProviderPage(
  client: PlatformAdminClient,
  serviceID: string,
  path: string,
  title: ReturnType<typeof message>,
): CollectionPageDefinition<ProviderRow> {
  let generation = 0;
  return defineCollectionPage<ProviderRow>({
    id: `authentication.providers.${serviceID}`,
    path,
    title,
    description: message(
      namespace,
      "page.description",
      "配置、测试、双人批准并发布企业认证 Provider",
    ),
    navigation: {
      id: `authentication.providers.${serviceID}`,
      label: title,
      zone: "settings",
      order: 15,
    },
    collection: {
      id: `authentication.providers.${serviceID}`,
      title,
      view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50] },
      filters: [
        {
          id: "id",
          label: message(namespace, "filter.id", "Profile ID"),
          kind: "text",
        },
      ],
      columns: [
        {
          key: "id",
          label: message(namespace, "column.id", "Profile"),
          defaultVisible: true,
          minWidth: 180,
        },
        {
          key: "contributionId",
          label: message(namespace, "column.provider", "Provider"),
          defaultVisible: true,
          minWidth: 180,
        },
        {
          key: "state",
          label: message(namespace, "column.state", "生命周期"),
          format: "status",
          defaultVisible: true,
          minWidth: 110,
        },
        {
          key: "readiness",
          label: message(namespace, "column.readiness", "就绪"),
          format: "status",
          defaultVisible: true,
          minWidth: 100,
        },
        {
          key: "updatedAt",
          label: message(namespace, "column.updated", "更新时间"),
          format: "datetime",
          defaultVisible: true,
          minWidth: 180,
        },
      ],
      actions: [
        {
          id: "create",
          label: message(namespace, "action.create", "新增 Provider"),
          placement: "page.primary",
          tone: "primary",
          form: "create",
        },
        {
          id: "publish",
          label: message(namespace, "action.publish", "发布目录"),
          placement: "page.secondary",
          form: "publish",
        },
        {
          id: "validate",
          label: message(namespace, "action.validate", "验证配置"),
          placement: "record.row",
        },
        {
          id: "test",
          label: message(namespace, "action.test", "认证测试"),
          placement: "record.row",
          form: "test",
        },
        {
          id: "approve",
          label: message(namespace, "action.approve", "批准"),
          placement: "record.row",
        },
        {
          id: "retire",
          label: message(namespace, "action.retire", "退役"),
          placement: "record.row",
          tone: "danger",
          confirm: message(
            namespace,
            "retire.confirm",
            "确认退役未被 Catalog 使用的 Provider？",
          ),
        },
      ],
    },
    forms: providerForms(client, () => generation),
    async load(query: CollectionQuery, signal) {
      const state: AuthenticationProviderManagementState =
        await client.authenticationProviderState();
      generation = state.generation;
      const rows: ProviderRow[] = state.providers.map((item) => ({
        ...item,
        generation: state.generation,
        id: item.profile.id,
        contributionId: item.profile.contributionId,
        state: item.lifecycle.state,
        readiness: item.lifecycle.readiness,
        updatedAt: item.lifecycle.updatedAt,
      }));
      const filter = String(query.filters.id ?? "").toLowerCase();
      const filtered = filter
        ? rows.filter((row) => row.id.toLowerCase().includes(filter))
        : rows;
      const start = (query.page - 1) * query.pageSize;
      return {
        items: signal.aborted
          ? []
          : filtered.slice(start, start + query.pageSize),
        total: filtered.length,
      };
    },
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      if (action.id === "validate")
        await client.validateAuthenticationProvider(row.id, row.generation);
      if (action.id === "approve")
        await client.approveAuthenticationProvider(row.id, row.generation);
      if (action.id === "retire")
        await client.retireAuthenticationProvider(row.id, row.generation);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}
