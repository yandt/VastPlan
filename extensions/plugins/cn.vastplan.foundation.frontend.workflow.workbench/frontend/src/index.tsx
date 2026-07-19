import type { UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import { CollectionPage } from "./patterns/collection/CollectionPage.js";

export const workbench: UIWorkbenchAdapter = {
  id: "ui.workflow.workbench",
  uiContract: "3.0.0",
  CollectionPage,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "action.refresh": "刷新", "action.clearFilters": "清除筛选", "action.columns": "列设置", "column.actions": "操作",
        "selection.count": "已选择 {count} 项", "empty.title": "暂无数据", "columns.title": "列设置", "action.done": "完成",
        "action.hide": "隐藏", "action.show": "显示", "column.unknown": "未知列",
      },
      "en-US": {
        "action.refresh": "Refresh", "action.clearFilters": "Clear filters", "action.columns": "Columns", "column.actions": "Actions",
        "selection.count": "{count} selected", "empty.title": "No data", "columns.title": "Columns", "action.done": "Done",
        "action.hide": "Hide", "action.show": "Show", "column.unknown": "Unknown column",
      },
    },
  },
};

export default workbench;
