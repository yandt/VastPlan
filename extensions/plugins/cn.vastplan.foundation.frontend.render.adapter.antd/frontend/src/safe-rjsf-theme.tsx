import { DeleteOutlined, HolderOutlined, PlusOutlined } from "@ant-design/icons";
import { DragDropProvider } from "@dnd-kit/react";
import { useSortable } from "@dnd-kit/react/sortable";
import { Button, Input, InputNumber, Tooltip, Typography } from "antd";
import type { ArrayFieldItemTemplateProps, ArrayFieldTemplateProps, BaseInputTemplateProps, DescriptionFieldProps, FieldHelpProps, TemplatesType } from "@rjsf/utils";
import { ariaDescribedByIds, getInputProps, helpId } from "@rjsf/utils";
import ArrayFieldItemTemplate from "@rjsf/antd/lib/templates/ArrayFieldItemTemplate/index.js";
import ArrayFieldTemplate from "@rjsf/antd/lib/templates/ArrayFieldTemplate/index.js";
import CyclicSchemaExpandTemplate from "@rjsf/antd/lib/templates/CyclicSchemaExpandTemplate/index.js";
import ErrorListTemplate from "@rjsf/antd/lib/templates/ErrorList/index.js";
import FieldErrorTemplate from "@rjsf/antd/lib/templates/FieldErrorTemplate/index.js";
import AntdFieldTemplate from "@rjsf/antd/lib/templates/FieldTemplate/index.js";
import GridTemplate from "@rjsf/antd/lib/templates/GridTemplate/index.js";
import { AddButton, ClearButton, CopyButton, MoveDownButton, MoveUpButton, RemoveButton } from "@rjsf/antd/lib/templates/IconButton/index.js";
import MultiSchemaFieldTemplate from "@rjsf/antd/lib/templates/MultiSchemaFieldTemplate/index.js";
import OptionalDataControlsTemplate from "@rjsf/antd/lib/templates/OptionalDataControlsTemplate/index.js";
import TitleFieldTemplate from "@rjsf/antd/lib/templates/TitleField/index.js";
import WrapIfAdditionalTemplate from "@rjsf/antd/lib/templates/WrapIfAdditionalTemplate/index.js";
import { createContext, useContext, useId, useRef } from "react";
import { message, usePortalI18n } from "@vastplan/ui-primitives";
import { namespace } from "./theme";

export const safeAntdTemplates: Partial<TemplatesType> = {
  ArrayFieldItemTemplate: CompactScalarArrayItem,
  ArrayFieldTemplate: CompactScalarArray,
  BaseInputTemplate,
  CyclicSchemaExpandTemplate,
  ButtonTemplates: { AddButton, ClearButton, CopyButton, MoveDownButton, MoveUpButton, RemoveButton, SubmitButton: () => null },
  DescriptionFieldTemplate,
  ErrorListTemplate,
  FieldErrorTemplate,
  FieldHelpTemplate,
  FieldTemplate: AntdFieldTemplate,
  GridTemplate,
  MultiSchemaFieldTemplate,
  OptionalDataControlsTemplate,
  TitleFieldTemplate,
  WrapIfAdditionalTemplate,
};

export { AntdFieldTemplate };

interface ArrayDragControls {
  group: string;
  reorder(source: number, target: number): void;
}

const inactiveArrayDragControls: ArrayDragControls = { group: "", reorder() {} };
const ArrayDragContext = createContext<ArrayDragControls>(inactiveArrayDragControls);

/** Keeps scalar list values visually lightweight while preserving RJSF's schema-owned array behavior. */
function CompactScalarArray(props: ArrayFieldTemplateProps) {
  const i18n = usePortalI18n();
  const group = useId();
  const itemsRef = useRef(props.items);
  const dragRef = useRef<{ source: number }>();
  const moveRef = useRef<(target: number) => void>();
  itemsRef.current = props.items;

  moveRef.current = (target) => {
    const source = dragRef.current?.source;
    if (source === undefined) return;
    if (source === target) { dragRef.current = undefined; return; }
    const item = itemsRef.current[source];
    const buttons = item?.props as ArrayFieldItemTemplateProps["buttonsProps"] | undefined;
    if (buttons === undefined) return;
    if (source < target && buttons.hasMoveDown) {
      buttons.onMoveDownItem();
      dragRef.current = { source: source + 1 };
    } else if (source > target && buttons.hasMoveUp) {
      buttons.onMoveUpItem();
      dragRef.current = { source: source - 1 };
    } else {
      dragRef.current = undefined;
      return;
    }
    if (dragRef.current.source !== target) window.requestAnimationFrame(() => moveRef.current?.(target));
  };

  if (!isScalarArray(props)) return <ArrayFieldTemplate {...props} />;
  const controls: ArrayDragControls = {
    group,
    reorder(source, target) { dragRef.current = { source }; moveRef.current?.(target); },
  };
  const disabled = props.disabled || props.readonly || !props.canAdd;
  return <DragDropProvider onDragEnd={(event) => {
    if (event.canceled) return;
    const source = arrayIndex(event.operation.source?.id);
    const target = arrayIndex(event.operation.target?.id);
    if (source !== undefined && target !== undefined) controls.reorder(source, target);
  }}><ArrayDragContext.Provider value={controls}>
      <div className="vp-antd-form-array" data-form-array="scalar">
        <div className="vp-antd-form-array-list">{props.items}</div>
        {props.canAdd ? <Button
          className="vp-antd-form-array-add"
          block
          type="dashed"
          disabled={disabled}
          icon={<PlusOutlined />}
          onClick={props.onAddClick}
        >{i18n.text(message(namespace, "form.list.add", "添加一项"))}</Button> : null}
      </div>
    </ArrayDragContext.Provider></DragDropProvider>;
}

function CompactScalarArrayItem(props: ArrayFieldItemTemplateProps) {
  if (!isScalarSchema(props.schema)) return <ArrayFieldItemTemplate {...props} />;
  return <SortableScalarArrayItem {...props} />;
}

function SortableScalarArrayItem(props: ArrayFieldItemTemplateProps) {
  const i18n = usePortalI18n();
  const drag = useContext(ArrayDragContext);
  const buttons = props.buttonsProps;
  const reorderable = !props.disabled && !props.readonly && (buttons.hasMoveUp || buttons.hasMoveDown);
  const sortable = useSortable({ id: String(props.index), index: props.index, group: drag.group, disabled: !reorderable });
  const removeLabel = i18n.text(message(namespace, "form.list.remove", "删除此项"));
  const reorderLabel = i18n.text(message(namespace, "form.list.reorder", "拖拽排序"));
  return <div
    className="vp-antd-form-array-item"
    data-array-index={props.index}
    data-dragging={sortable.isDragging || undefined}
    data-drop-target={sortable.isDropTarget || undefined}
    ref={sortable.ref}
  >
    <Tooltip title={reorderLabel}><button
      className="vp-antd-form-array-drag"
      type="button"
      aria-label={reorderLabel}
      aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
      disabled={!reorderable}
      data-array-drag-handle
      ref={sortable.handleRef}
      onKeyDown={(event) => {
        if (!event.altKey) return;
        if (event.key === "ArrowUp" && buttons.hasMoveUp) { event.preventDefault(); buttons.onMoveUpItem(); }
        if (event.key === "ArrowDown" && buttons.hasMoveDown) { event.preventDefault(); buttons.onMoveDownItem(); }
      }}
    ><HolderOutlined /></button></Tooltip>
    <div className="vp-antd-form-array-value">{props.children}</div>
    {buttons.hasRemove ? <Tooltip title={removeLabel}><Button
      className="vp-antd-form-array-remove"
      type="text"
      danger
      aria-label={removeLabel}
      icon={<DeleteOutlined />}
      disabled={props.disabled || props.readonly}
      onClick={buttons.onRemoveItem}
    /></Tooltip> : <span className="vp-antd-form-array-action-spacer" aria-hidden />}
  </div>;
}

function arrayIndex(id: unknown): number | undefined {
  if (typeof id !== "string" && typeof id !== "number") return undefined;
  const index = Number(id);
  return Number.isInteger(index) && index >= 0 ? index : undefined;
}

function isScalarArray(props: ArrayFieldTemplateProps): boolean {
  const items = (props.schema as { items?: unknown }).items;
  return typeof items === "object" && items !== null && !Array.isArray(items) && isScalarSchema(items as { type?: unknown });
}

function isScalarSchema(schema: { type?: unknown }): boolean {
  return schema.type === "string" || schema.type === "number" || schema.type === "integer" || schema.type === "boolean";
}

export const antdArrayFieldCSS = `
.vp-antd-form-array{display:grid;gap:6px;width:100%;min-width:0}
.vp-antd-form-array-list{display:grid;gap:6px;width:100%;min-width:0}
.vp-antd-form-array-item{display:grid;grid-template-columns:28px minmax(0,1fr) 28px;align-items:start;gap:6px;width:100%;min-width:0}
.vp-antd-form-array-item[data-dragging=true]{opacity:.6}
.vp-antd-form-array-item[data-drop-target=true]{outline:1px solid var(--ant-color-primary);outline-offset:2px}
.vp-antd-form-array-item .ant-form-item{width:100%;margin:0!important}
.vp-antd-form-array-item .ant-form-item-label{display:none!important}
.vp-antd-form-array-item .ant-form-item-control,.vp-antd-form-array-item .ant-form-item-control-input,.vp-antd-form-array-item .ant-form-item-control-input-content{width:100%;min-width:0}
.vp-antd-form-array-item .ant-form-item-control-input-content>div{width:100%;min-width:0}
.vp-antd-form-array-value{min-width:0}
.vp-antd-form-array-drag{display:grid;place-items:center;width:28px;height:32px;padding:0;border:0;border-radius:var(--ant-border-radius);color:var(--ant-color-text-tertiary);background:transparent;cursor:grab}
.vp-antd-form-array-drag:hover:not(:disabled),.vp-antd-form-array-drag:focus-visible{color:var(--ant-color-primary);background:var(--ant-color-primary-bg)}
.vp-antd-form-array-drag:active{cursor:grabbing}
.vp-antd-form-array-drag:disabled{cursor:default;opacity:.45}
.vp-antd-form-array-drag:focus-visible{outline:2px solid var(--ant-color-primary);outline-offset:1px}
.vp-antd-form-array-remove,.vp-antd-form-array-action-spacer{width:28px;height:32px}
.vp-antd-form-array-add{width:100%;margin-top:2px}
`;

function BaseInputTemplate({ disabled, id, htmlName, onBlur, onChange, onChangeOverride, onFocus, options, placeholder, readonly, required, schema, value, type }: BaseInputTemplateProps) {
  const inputProps = getInputProps(schema, type, options, false);
  const handleBlur = (targetValue: unknown) => onBlur(id, targetValue);
  const handleFocus = (targetValue: unknown) => onFocus(id, targetValue);
  if (inputProps.type === "number" || inputProps.type === "integer") return <InputNumber
    id={id}
    name={htmlName || id}
    value={value}
    disabled={disabled || readonly}
    required={required}
    placeholder={placeholder}
    style={{ width: "100%" }}
    min={typeof inputProps.min === "number" ? inputProps.min : undefined}
    max={typeof inputProps.max === "number" ? inputProps.max : undefined}
    step={typeof inputProps.step === "number" ? inputProps.step : undefined}
    aria-describedby={ariaDescribedByIds(id, !!schema.examples)}
    onChange={readonly ? undefined : (next) => onChange(next)}
    onBlur={readonly ? undefined : (event) => handleBlur(event.target.value)}
    onFocus={readonly ? undefined : (event) => handleFocus(event.target.value)}
  />;
  return <Input
    {...inputProps}
    id={id}
    name={htmlName || id}
    value={value ?? ""}
    disabled={disabled || readonly}
    required={required}
    placeholder={placeholder}
    aria-describedby={ariaDescribedByIds(id, !!schema.examples)}
    onChange={readonly ? undefined : onChangeOverride ?? ((event) => onChange(event.target.value === "" ? options.emptyValue : event.target.value))}
    onBlur={readonly ? undefined : (event) => handleBlur(event.target.value)}
    onFocus={readonly ? undefined : (event) => handleFocus(event.target.value)}
  />;
}

function DescriptionFieldTemplate({ id, description }: DescriptionFieldProps) {
  return description ? <Typography.Text id={id} type="secondary">{description as React.ReactNode}</Typography.Text> : null;
}

function FieldHelpTemplate({ fieldPathId, help }: FieldHelpProps) {
  return help ? <Typography.Text id={helpId(fieldPathId)} type="secondary">{help as React.ReactNode}</Typography.Text> : null;
}
