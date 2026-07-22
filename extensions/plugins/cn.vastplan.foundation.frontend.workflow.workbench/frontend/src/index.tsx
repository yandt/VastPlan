import type { UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import { CollectionPage } from "./patterns/collection/CollectionPage.js";
import { CollectionPageActions } from "./patterns/collection/CollectionPageActions.js";
import { FormPage } from "./patterns/form/FormPage.js";
import { RecordPage } from "./patterns/record/RecordPage.js";
import { RecordPageActions } from "./patterns/record/RecordPageActions.js";

export const workbench: UIWorkbenchAdapter = {
  id: "ui.workflow.workbench",
  uiContract: "4.0.0",
  CollectionPage,
  CollectionPageActions,
  FormPage,
  RecordPage,
  RecordPageActions,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "action.refresh": "刷新", "action.clearFilters": "清除筛选", "action.columns": "列设置", "action.more": "更多页面操作", "column.actions": "操作",
        "selection.count": "已选择 {count} 项", "bulk.select": "选择批量操作", "bulk.placeholder": "选择批量操作", "bulk.execute": "执行", "empty.title": "暂无数据", "columns.title": "列设置", "action.done": "完成",
        "action.hide": "隐藏", "action.show": "显示", "column.unknown": "未知列", "density.title": "显示密度", "density.compact": "紧凑", "density.standard": "标准", "density.comfortable": "宽松", "preference.saveFailed": "显示偏好保存失败",
        "selection.card": "选择 {title}", "cursor.more": "加载更多", "cursor.loading": "正在加载更多",
        "form.discardTitle": "放弃未保存的修改？", "form.discardContent": "关闭后，本次输入不会保留。", "form.secretLoadRejected": "一次性秘密字段禁止从存储中回填；已安全丢弃该值。", "action.cancel": "取消", "action.submit": "提交", "action.previous": "上一步", "action.next": "下一步",
        "record.empty": "请选择一条记录", "record.back": "返回列表", "record.selectionDiscard": "切换记录后，当前未保存修改不会保留。", "record.masterEmpty": "暂无记录", "record.treeEmpty": "暂无节点",
        "value.yes": "是", "value.no": "否",
      },
      "en-US": {
        "action.refresh": "Refresh", "action.clearFilters": "Clear filters", "action.columns": "Columns", "action.more": "More page actions", "column.actions": "Actions",
        "selection.count": "{count} selected", "bulk.select": "Select bulk action", "bulk.placeholder": "Select bulk action", "bulk.execute": "Run", "empty.title": "No data", "columns.title": "Columns", "action.done": "Done",
        "action.hide": "Hide", "action.show": "Show", "column.unknown": "Unknown column", "density.title": "Display density", "density.compact": "Compact", "density.standard": "Standard", "density.comfortable": "Comfortable", "preference.saveFailed": "Could not save display preferences",
        "selection.card": "Select {title}", "cursor.more": "Load more", "cursor.loading": "Loading more",
        "form.discardTitle": "Discard unsaved changes?", "form.discardContent": "Your current input will not be kept.", "form.secretLoadRejected": "One-time secret material cannot be loaded from storage; the value was discarded safely.", "action.cancel": "Cancel", "action.submit": "Submit", "action.previous": "Previous", "action.next": "Next",
        "record.empty": "Select a record", "record.back": "Back to list", "record.selectionDiscard": "Switching records will discard your unsaved changes.", "record.masterEmpty": "No records", "record.treeEmpty": "No nodes",
        "value.yes": "Yes", "value.no": "No",
      },
    },
  },
};

export default workbench;
