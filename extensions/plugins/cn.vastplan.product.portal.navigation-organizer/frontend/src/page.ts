import { defineCollectionPage, jsonSchemaDialect, message, type CollectionPageDefinition, type CollectionQuery, type WorkbenchFormDefinition, type WorkbenchFormFieldErrors, type WorkbenchFormSubmitResult } from "@vastplan/workbench-sdk";
import type { NavigationFolder, NavigationOrganizerClient } from "./management-client.js";

const namespace = "cn.vastplan.product.portal.navigation-organizer";
const iconNames = ["folder", "menu", "settings", "portal", "plugins", "resources", "extension", "workbench"] as const;

interface FolderRow extends Record<string, unknown> {
  id: string;
  serviceId: string;
  label: string;
  iconName: string;
  members: readonly string[];
  memberSummary: string;
  memberCount: number;
  order?: number;
  activationId: number;
}

interface FolderFormValue extends Record<string, unknown> {
  id?: string;
  label?: string;
  iconName?: string;
  members?: readonly string[];
  order?: number;
}

const folderSchema = {
  id: "portal-navigation-folder.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["id", "label", "members"],
    properties: {
      id: { type: "string", title: "文件夹 ID", pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$", maxLength: 160 },
      label: { type: "string", title: "文件夹名称", minLength: 1, maxLength: 80 },
      iconName: { type: "string", title: "图标", enum: ["", ...iconNames], enumNames: ["自动组合前四个菜单图标", ...iconNames] },
      members: { type: "array", title: "根菜单", minItems: 2, maxItems: 64, uniqueItems: true, items: { type: "string", minLength: 3, maxLength: 321 } },
      order: { type: "integer", title: "顺序", minimum: -1000000, maximum: 1000000 },
    },
  },
  localization: {
    "/properties/id/title": message(namespace, "form.id", "文件夹 ID"),
    "/properties/label/title": message(namespace, "form.label", "文件夹名称"),
    "/properties/iconName/title": message(namespace, "form.icon", "图标"),
    "/properties/members/title": message(namespace, "form.members", "根菜单"),
    "/properties/order/title": message(namespace, "form.order", "顺序"),
  },
} as const;

export function createNavigationFolderPage(client: NavigationOrganizerClient, serviceID: string, title: ReturnType<typeof message>): CollectionPageDefinition<FolderRow> {
  const form = (id: "create" | "edit"): WorkbenchFormDefinition<FolderRow> => ({
    id, schema: folderSchema,
    presentation: {
      layout: "vertical", navigation: "sections",
      sections: [{ id: "folder", title, columns: 2, fields: ["/id", "/label", "/iconName", "/order", "/members"] }],
      fields: [
        { pointer: "/id", readOnlyWhen: { pointer: "/context/editing", equals: true } },
        { pointer: "/label" }, { pointer: "/iconName", widget: "select" }, { pointer: "/order", widget: "number" },
        { pointer: "/members", span: 2, help: message(namespace, "form.membersHelp", "每项填写一个插件根菜单 ID，例如 cn.example.plugin/root。至少选择两个根菜单。") },
      ],
    },
    context: { editing: id === "edit" },
    workflow: {
      title: message(namespace, id === "edit" ? "form.editTitle" : "form.createTitle", id === "edit" ? "编辑导航文件夹" : "新建导航文件夹"),
      dialogWidth: "md", submitLabel: message(namespace, "action.publish", "发布"),
      success: { notify: message(namespace, "notice.published", "导航编排已发布"), refreshCollection: true, close: true },
    },
    initialValue: { iconName: "", members: [] },
    async load(selected) {
      const row = selected[0];
      return row === undefined ? { iconName: "", members: [] } : { id: row.id, label: row.label, iconName: row.iconName, members: [...row.members], ...(row.order === undefined ? {} : { order: row.order }) };
    },
    async validate({ value }): Promise<WorkbenchFormFieldErrors> {
      const members = Array.isArray(value.members) ? value.members.filter((item): item is string => typeof item === "string" && item.trim() !== "") : [];
      return members.length < 2 ? { members: message(namespace, "error.members", "文件夹至少需要两个根菜单") } : {};
    },
    async submit({ value, selected }): Promise<WorkbenchFormSubmitResult | void> {
      const normalized = normalizeFolder(value, serviceID);
      if (normalized === undefined) return { fieldErrors: { id: message(namespace, "error.required", "请完整填写文件夹信息") } };
      const snapshot = await client.read();
      const selectedID = selected[0]?.id;
      if (selectedID === undefined && snapshot.folders.some((folder) => folder.id === normalized.id)) return { fieldErrors: { id: message(namespace, "error.duplicate", "文件夹 ID 已存在") } };
      const folders = selectedID === undefined ? [...snapshot.folders, normalized] : snapshot.folders.map((folder) => folder.id === selectedID ? normalized : folder);
      await client.publish(snapshot.activationId, folders);
    },
  });

  return defineCollectionPage<FolderRow>({
    id: `portal.navigation-organizer.${serviceID}`,
    path: `/settings/navigation/${encodeURIComponent(serviceID)}`,
    title,
    description: message(namespace, "page.description", "将多个插件根菜单收纳为展示文件夹，不改变 root-child-page 语义层级"),
    requiredPermissions: ["portal.navigation.read"],
    pageActions: [{ id: `create.${serviceID}`, label: message(namespace, "action.create", "新建文件夹"), icon: "add", tone: "primary", form: "create", requiredPermissions: ["portal.navigation.publish"] }],
    collection: {
      id: `portal.navigation-organizer.${serviceID}`, title, view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50] },
      columns: [
        { key: "label", label: message(namespace, "column.label", "名称"), defaultVisible: true, minWidth: 180 },
        { key: "iconName", label: message(namespace, "column.icon", "图标"), defaultVisible: true, minWidth: 120 },
        { key: "memberCount", label: message(namespace, "column.count", "菜单数"), format: "number", defaultVisible: true, minWidth: 90 },
        { key: "memberSummary", label: message(namespace, "column.members", "根菜单"), defaultVisible: true, minWidth: 320 },
        { key: "order", label: message(namespace, "column.order", "顺序"), format: "number", defaultVisible: true, minWidth: 90 },
      ],
      actions: [
        { id: `edit.${serviceID}`, label: message(namespace, "action.edit", "编辑"), icon: "edit", placement: "record.row", form: "edit", requiredPermissions: ["portal.navigation.publish"] },
        { id: `delete.${serviceID}`, label: message(namespace, "action.delete", "删除"), icon: "remove", placement: "record.row", tone: "danger", requiredPermissions: ["portal.navigation.publish"], confirm: message(namespace, "confirm.delete", "删除此文件夹并发布新的 Portal Generation？") },
      ],
    },
    forms: [form("create"), form("edit")],
    async load(query: CollectionQuery, signal) {
      const snapshot = await client.read();
      if (signal.aborted) return { items: [], total: 0 };
      const rows = snapshot.folders.map((folder) => row(folder, snapshot.activationId));
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      if (action.id !== `delete.${serviceID}` || selected[0] === undefined) return;
      const snapshot = await client.read();
      await client.publish(snapshot.activationId, snapshot.folders.filter((folder) => folder.id !== selected[0]!.id));
    },
  });
}

function row(folder: NavigationFolder, activationId: number): FolderRow {
  return {
    id: folder.id, serviceId: folder.serviceId, label: folder.label, iconName: folder.icon?.name ?? "自动组合",
    members: folder.members, memberSummary: folder.members.join(", "), memberCount: folder.members.length,
    ...(folder.order === undefined ? {} : { order: folder.order }), activationId,
  };
}

function normalizeFolder(value: FolderFormValue, serviceID: string): NavigationFolder | undefined {
  const id = typeof value.id === "string" ? value.id.trim() : "";
  const label = typeof value.label === "string" ? value.label.trim() : "";
  const members = Array.isArray(value.members) ? [...new Set(value.members.filter((item): item is string => typeof item === "string").map((item) => item.trim()).filter(Boolean))] : [];
  if (id === "" || label === "" || members.length < 2) return undefined;
  const iconName = typeof value.iconName === "string" && value.iconName !== "" ? value.iconName : undefined;
  return { id, serviceId: serviceID, label, members, ...(iconName === undefined ? {} : { icon: { kind: "semantic", name: iconName } }), ...(Number.isSafeInteger(value.order) ? { order: Number(value.order) } : {}) };
}
