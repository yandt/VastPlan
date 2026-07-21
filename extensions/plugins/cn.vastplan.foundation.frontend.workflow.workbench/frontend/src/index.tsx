import type { UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import { CollectionPage } from "./patterns/collection/CollectionPage.js";
import { FormPage } from "./patterns/form/FormPage.js";

export const workbench: UIWorkbenchAdapter = {
  id: "ui.workflow.workbench",
  uiContract: "4.0.0",
  CollectionPage,
  FormPage,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "action.refresh": "刷新", "action.clearFilters": "清除筛选", "action.columns": "列设置", "column.actions": "操作",
        "selection.count": "已选择 {count} 项", "empty.title": "暂无数据", "columns.title": "列设置", "action.done": "完成",
        "action.hide": "隐藏", "action.show": "显示", "column.unknown": "未知列",
        "selection.card": "选择 {title}", "cursor.more": "加载更多", "cursor.loading": "正在加载更多",
        "form.discardTitle": "放弃未保存的修改？", "form.discardContent": "关闭后，本次输入不会保留。", "form.secretLoadRejected": "一次性秘密字段禁止从存储中回填；已安全丢弃该值。", "action.cancel": "取消", "action.submit": "提交", "action.previous": "上一步", "action.next": "下一步",
        "value.yes": "是", "value.no": "否",
      },
      "en-US": {
        "action.refresh": "Refresh", "action.clearFilters": "Clear filters", "action.columns": "Columns", "column.actions": "Actions",
        "selection.count": "{count} selected", "empty.title": "No data", "columns.title": "Columns", "action.done": "Done",
        "action.hide": "Hide", "action.show": "Show", "column.unknown": "Unknown column",
        "selection.card": "Select {title}", "cursor.more": "Load more", "cursor.loading": "Loading more",
        "form.discardTitle": "Discard unsaved changes?", "form.discardContent": "Your current input will not be kept.", "form.secretLoadRejected": "One-time secret material cannot be loaded from storage; the value was discarded safely.", "action.cancel": "Cancel", "action.submit": "Submit", "action.previous": "Previous", "action.next": "Next",
        "value.yes": "Yes", "value.no": "No",
      },
    },
  },
};

export default workbench;
