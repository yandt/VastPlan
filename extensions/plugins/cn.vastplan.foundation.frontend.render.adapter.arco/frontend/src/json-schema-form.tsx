import {
  Alert,
  Button,
  Card,
  ColorPicker,
  DatePicker,
  Form,
  Grid as ArcoGrid,
  Input,
  InputNumber,
  Rate,
  Select,
  Space,
  Tabs,
  Switch,
  TimePicker,
  Typography,
  IconCopy,
  IconDelete,
  IconDown,
  IconPlus,
  IconUp,
} from "./arco-components";
// RJSF 6.7 的根入口会静态导入仅供测试使用的 AJV8 Validator。
// 生产 Renderer 直接使用公开 Form 子路径，保持自有 CSP Validator 为唯一验证器。
import RJSFForm from "@rjsf/core/lib/components/Form.js";
import {
  canExpand,
  enumOptionsIndexForValue,
  enumOptionsValueForIndex,
} from "@rjsf/utils";
import type {
  ArrayFieldItemButtonsTemplateProps,
  ArrayFieldItemTemplateProps,
  ArrayFieldTemplateProps,
  BaseInputTemplateProps,
  ErrorListProps,
  ErrorSchema,
  FieldTemplateProps,
  IconButtonProps,
  MultiSchemaFieldTemplateProps,
  ObjectFieldTemplateProps,
  RegistryWidgetsType,
  RJSFSchema,
  RJSFValidationError,
  TemplatesType,
  UiSchema,
  WidgetProps,
  WrapIfAdditionalTemplateProps,
} from "@rjsf/utils";
import { useEffect, useMemo, useState } from "react";
import { cspJSONSchemaValidator } from "@vastplan/rjsf-csp-validator";
import type { FormPresentation, FormRendererProps, FormSectionPresentation, FormValidationIssue } from "@vastplan/ui-primitives";
import { jsonSchemaDialect, localizeJSONSchema, message, usePortalI18n } from "@vastplan/ui-primitives";

type FormData = Record<string, unknown>;
type FormContext = Readonly<Record<string, unknown>>;
type Schema = RJSFSchema;

const emptyContext: FormContext = {};
const namespace = "cn.vastplan.foundation.frontend.render.adapter";

export const arcoJSONSchemaValidator = cspJSONSchemaValidator;

function controlDisabled(props: Pick<WidgetProps, "disabled" | "readonly">): boolean {
  return Boolean(props.disabled || props.readonly);
}

function BaseInputTemplate({ id, value, placeholder, required, disabled, readonly, autofocus, options, onChange, onBlur, onFocus }: BaseInputTemplateProps) {
  const common = {
    id,
    value: value ?? "",
    placeholder,
    required,
    disabled: controlDisabled({ disabled, readonly }),
    autoFocus: autofocus,
    allowClear: options.allowClear === true,
    onChange: (next: string) => onChange(next === "" ? options.emptyValue : next),
    onBlur: () => onBlur(id, value),
    onFocus: () => onFocus(id, value),
  };
  return options.inputType === "password" ? <Input.Password {...common} autoComplete="new-password" /> : <Input {...common} />;
}

function TextareaWidget({ id, value, placeholder, required, disabled, readonly, autofocus, options, onChange, onBlur, onFocus }: WidgetProps) {
  return <Input.TextArea
    id={id}
    value={value ?? ""}
    placeholder={placeholder}
    required={required}
    disabled={controlDisabled({ disabled, readonly })}
    autoFocus={autofocus}
    autoSize={options.rows === undefined ? { minRows: 3, maxRows: 10 } : undefined}
    rows={typeof options.rows === "number" ? options.rows : undefined}
    onChange={(next) => onChange(next === "" ? options.emptyValue : next)}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  />;
}

function PasswordWidget({ id, value, placeholder, required, disabled, readonly, autofocus, options, onChange, onBlur, onFocus }: WidgetProps) {
  return <Input.Password
    id={id}
    value={typeof value === "string" ? value : ""}
    placeholder={placeholder}
    required={required}
    disabled={controlDisabled({ disabled, readonly })}
    autoFocus={autofocus}
    autoComplete="new-password"
    onChange={(next) => onChange(next === "" ? options.emptyValue : next)}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  />;
}

function CheckboxWidget({ id, value, disabled, readonly, autofocus, onChange, onBlur, onFocus }: WidgetProps) {
  return <Switch
    id={id}
    checked={value === true}
    disabled={controlDisabled({ disabled, readonly })}
    autoFocus={autofocus}
    onChange={onChange}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  />;
}

function SelectWidget({ id, value, multiple, placeholder, disabled, readonly, options, onChange, onBlur, onFocus }: WidgetProps) {
  const enumOptions = options.enumOptions ?? [];
  const selected = enumOptionsIndexForValue(value, enumOptions, multiple);
  return <Select
    id={id}
    value={selected}
    mode={multiple ? "multiple" : undefined}
    placeholder={placeholder}
    disabled={controlDisabled({ disabled, readonly })}
    allowClear
    options={enumOptions.map((option, index) => ({ label: option.label, value: String(index) }))}
    onChange={(next) => onChange(next === undefined ? options.emptyValue : enumOptionsValueForIndex(next, enumOptions, options.emptyValue))}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  />;
}

function MultiSelectWidget(props: WidgetProps) {
  return <SelectWidget {...props} multiple />;
}

function NumberWidget({ id, value, placeholder, required, disabled, readonly, autofocus, options, schema, onChange, onBlur, onFocus }: WidgetProps) {
  return <InputNumber
    id={id}
    value={typeof value === "number" ? value : undefined}
    placeholder={placeholder}
    required={required}
    disabled={controlDisabled({ disabled, readonly })}
    autoFocus={autofocus}
    min={typeof schema.minimum === "number" ? schema.minimum : undefined}
    max={typeof schema.maximum === "number" ? schema.maximum : undefined}
    step={typeof schema.multipleOf === "number" ? schema.multipleOf : undefined}
    precision={typeof options.precision === "number" ? options.precision : undefined}
    onChange={(next) => onChange(next ?? options.emptyValue)}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  />;
}

function DateWidget({ id, value, placeholder, disabled, readonly, onChange, onBlur, onFocus }: WidgetProps) {
  return <div onBlur={() => onBlur(id, value)} onFocus={() => onFocus(id, value)}>
    <DatePicker
      value={typeof value === "string" ? value : undefined}
      placeholder={placeholder}
      disabled={controlDisabled({ disabled, readonly })}
      onChange={(next) => onChange(next || undefined)}
    />
  </div>;
}

function DateTimeWidget(props: WidgetProps) {
  return <div onBlur={() => props.onBlur(props.id, props.value)} onFocus={() => props.onFocus(props.id, props.value)}>
    <DatePicker
      showTime
      value={typeof props.value === "string" ? props.value : undefined}
      placeholder={props.placeholder}
      disabled={controlDisabled(props)}
      onChange={(next) => props.onChange(next || undefined)}
    />
  </div>;
}

function TimeWidget(props: WidgetProps) {
  return <div onBlur={() => props.onBlur(props.id, props.value)} onFocus={() => props.onFocus(props.id, props.value)}>
    <TimePicker
      value={typeof props.value === "string" ? props.value : undefined}
      placeholder={props.placeholder}
      disabled={controlDisabled(props)}
      onChange={(next) => props.onChange(next || undefined)}
    />
  </div>;
}

function ColorWidget(props: WidgetProps) {
  return <div onBlur={() => props.onBlur(props.id, props.value)} onFocus={() => props.onFocus(props.id, props.value)}>
    <ColorPicker
      value={typeof props.value === "string" ? props.value : undefined}
      disabled={controlDisabled(props)}
      showText
      onChange={(next) => props.onChange(typeof next === "string" ? next : undefined)}
    />
  </div>;
}

function RatingWidget(props: WidgetProps) {
  return <Rate
    value={typeof props.value === "number" ? props.value : undefined}
    count={typeof props.schema.maximum === "number" ? props.schema.maximum : 5}
    disabled={props.disabled}
    readonly={props.readonly}
    allowHalf={props.schema.multipleOf === 0.5}
    onChange={props.onChange}
  />;
}

function UnsupportedFileWidget() {
  const i18n = usePortalI18n();
  return <Alert type="warning" content={i18n.text(message(namespace,"form.fileUnsupported","表单不接受内嵌文件数据；请使用受控制品上传能力。"))} />;
}

function SecretRefWidget(props: WidgetProps) {
  const i18n = usePortalI18n();
  if ((props.options.enumOptions?.length ?? 0) > 0) return <SelectWidget {...props} />;
  return <Input
    id={props.id}
    value={typeof props.value === "string" ? props.value : ""}
    placeholder={props.placeholder ?? i18n.text(message(namespace,"form.credentialPlaceholder","输入 credential:// 凭证引用（禁止填写明文）"))}
    disabled={controlDisabled(props)}
    autoComplete="off"
    onChange={(next) => props.onChange(next || undefined)}
    onBlur={() => props.onBlur(props.id, props.value)}
    onFocus={() => props.onFocus(props.id, props.value)}
  />;
}

function HiddenWidget({ id, value }: WidgetProps) {
  return <input id={id} type="hidden" value={value ?? ""} readOnly />;
}

export const arcoFormWidgets: RegistryWidgetsType<FormData, Schema, FormContext> = {
  TextWidget: BaseInputTemplate,
  PasswordWidget,
  EmailWidget: BaseInputTemplate,
  URLWidget: BaseInputTemplate,
  TextareaWidget,
  CheckboxWidget,
  SelectWidget,
  RadioWidget: SelectWidget,
  CheckboxesWidget: MultiSelectWidget,
  UpDownWidget: NumberWidget,
  RangeWidget: NumberWidget,
  DateWidget,
  DateTimeWidget,
  TimeWidget,
  ColorWidget,
  RatingWidget,
  FileWidget: UnsupportedFileWidget,
  AltDateWidget: DateWidget,
  AltDateTimeWidget: DateTimeWidget,
  secretRef: SecretRefWidget,
  HiddenWidget,
};

function FieldTemplate({ label, children, rawDescription, rawHelp, rawErrors, hidden, required, displayLabel, schema }: FieldTemplateProps) {
  if (hidden) return <div style={{ display: "none" }}>{children}</div>;
  if (schema.type === "object" || schema.type === "array") return <>
    {children}
    {rawHelp === undefined ? null : <Typography.Paragraph type="secondary">{rawHelp}</Typography.Paragraph>}
  </>;
  return <Form.Item
    label={displayLabel === false ? undefined : label}
    required={required}
    extra={rawHelp ?? rawDescription}
    validateStatus={(rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={rawErrors?.[0]}
  >{children}</Form.Item>;
}

function ObjectFieldTemplate({ title, description, properties, schema, uiSchema, formData, onAddProperty, fieldPathId, readonly, disabled }: ObjectFieldTemplateProps) {
  const i18n = usePortalI18n();
  const content = <>{properties.filter((property) => !property.hidden).map((property) => property.content)}</>;
  const add = canExpand(schema, uiSchema, formData) && !readonly && !disabled
    ? <Button size="small" icon={<IconPlus />} onClick={onAddProperty}>{i18n.text(message(namespace,"form.addProperty","添加属性"))}</Button>
    : undefined;
  if (fieldPathId.path.length === 0) return <>
    {title === "" ? null : <Typography.Title heading={6}>{title}</Typography.Title>}
    {description}
    {content}
    {add}
  </>;
  return <Card title={title} size="small" style={{ marginBottom: 16 }} extra={add}>
    {description}
    {content}
  </Card>;
}

function PresentedObjectFieldTemplate({ presentation, activeSection, onSectionChange, ...props }: ObjectFieldTemplateProps & { presentation?: FormPresentation; activeSection?: string; onSectionChange?(sectionID: string): void }) {
  if (props.fieldPathId.path.length !== 0 || presentation?.sections === undefined || presentation.sections.length === 0) return <ObjectFieldTemplate {...props} />;
  const i18n = usePortalI18n();
  const sections = presentation.sections;
  const selected = sections.find((section) => section.id === activeSection) ?? sections[0]!;
  const assigned = new Set(sections.flatMap((section) => section.fields.map(fieldName)));
  const remainder = props.properties.filter((property) => !assigned.has(property.name));
  const section = (value: FormSectionPresentation) => {
    const fields = value.fields.map(fieldName);
    const content = <ArcoGrid cols={value.columns ?? 1} rowGap={12} colGap={16}>{props.properties.filter((property) => fields.includes(property.name) && !property.hidden).map((property) => {
      const span = presentation.fields?.find((field) => fieldName(field.pointer) === property.name)?.span ?? 1;
      return <ArcoGrid.GridItem key={property.name} span={Math.min(Math.max(1, span), value.columns ?? 1)}>{property.content}</ArcoGrid.GridItem>;
    })}</ArcoGrid>;
    const body = <>{value.description === undefined ? null : <Typography.Paragraph type="secondary">{i18n.text(value.description)}</Typography.Paragraph>}{content}</>;
    if (presentation.navigation !== "sections") return body;
    return value.collapsible
      ? <details><summary style={{ cursor: "pointer", fontWeight: 600, marginBottom: 12 }}>{value.title === undefined ? value.id : i18n.text(value.title)}</summary>{body}</details>
      : <Card title={value.title === undefined ? undefined : i18n.text(value.title)} size="small">{body}</Card>;
  };
  const remaining = remainder.length === 0 ? null : <ArcoGrid cols={1} rowGap={12}>{remainder.map((property) => <ArcoGrid.GridItem key={property.name}>{property.content}</ArcoGrid.GridItem>)}</ArcoGrid>;
  if (presentation.navigation === "tabs") return <>{props.title === "" ? null : <Typography.Title heading={6}>{props.title}</Typography.Title>}<Tabs activeTab={selected.id} onChange={onSectionChange}>{sections.map((item) => <Tabs.TabPane key={item.id} title={item.title === undefined ? item.id : i18n.text(item.title)}>{section(item)}</Tabs.TabPane>)}</Tabs>{remaining}</>;
  if (presentation.navigation === "steps") {
    const current = Math.max(0, sections.findIndex((item) => item.id === selected.id));
    return <>{props.title === "" ? null : <Typography.Title heading={6}>{props.title}</Typography.Title>}<ol aria-label={props.title} style={{ display: "flex", gap: 8, margin: "0 0 20px", padding: 0, listStyle: "none" }}>{sections.map((item, index) => <li key={item.id} aria-current={index === current ? "step" : undefined}><Button type={index === current ? "primary" : "secondary"} onClick={() => onSectionChange?.(item.id)}>{index + 1}. {item.title === undefined ? item.id : i18n.text(item.title)}</Button></li>)}</ol>{section(selected)}{remaining}</>;
  }
  return <Space direction="vertical" size={16} style={{ width: "100%" }}>{sections.map((item) => <div key={item.id}>{section(item)}</div>)}{remaining}</Space>;
}

function fieldName(pointer: string): string {
  const first = pointer.startsWith("/") ? pointer.slice(1).split("/")[0] ?? "" : pointer;
  return first.replace(/~1/g, "/").replace(/~0/g, "~");
}

function ArrayFieldTemplate({ title, items, canAdd, onAddClick, readonly, disabled, rawErrors }: ArrayFieldTemplateProps) {
  const i18n = usePortalI18n();
  return <Card
    title={title}
    size="small"
    style={{ marginBottom: 16 }}
    extra={canAdd && !readonly && !disabled ? <Button size="small" icon={<IconPlus />} onClick={onAddClick}>{i18n.text(message(namespace,"form.add","添加"))}</Button> : undefined}
  >
    {(rawErrors?.length ?? 0) > 0 ? <Alert type="error" content={rawErrors?.[0]} style={{ marginBottom: 12 }} /> : null}
    {items.length === 0 ? <Typography.Text type="secondary">{i18n.text(message(namespace,"form.empty","暂无条目"))}</Typography.Text> : items}
  </Card>;
}

function ArrayFieldItemButtonsTemplate({ hasMoveUp, hasMoveDown, hasCopy, hasRemove, disabled, readonly, onMoveUpItem, onMoveDownItem, onCopyItem, onRemoveItem }: ArrayFieldItemButtonsTemplateProps) {
  const i18n = usePortalI18n();
  const locked = Boolean(disabled || readonly);
  return <Space size={4}>
    {hasMoveUp ? <Button size="mini" icon={<IconUp />} disabled={locked} aria-label={i18n.text(message(namespace,"action.moveUp","上移"))} onClick={onMoveUpItem} /> : null}
    {hasMoveDown ? <Button size="mini" icon={<IconDown />} disabled={locked} aria-label={i18n.text(message(namespace,"action.moveDown","下移"))} onClick={onMoveDownItem} /> : null}
    {hasCopy ? <Button size="mini" icon={<IconCopy />} disabled={locked} aria-label={i18n.text(message(namespace,"action.copy","复制"))} onClick={onCopyItem} /> : null}
    {hasRemove ? <Button size="mini" status="danger" icon={<IconDelete />} disabled={locked} aria-label={i18n.text(message(namespace,"action.delete","删除"))} onClick={onRemoveItem} /> : null}
  </Space>;
}

function ArrayFieldItemTemplate({ children, buttonsProps, index, hasToolbar }: ArrayFieldItemTemplateProps) {
  const i18n = usePortalI18n();
  return <Card
    key={buttonsProps.fieldPathId.$id}
    title={i18n.text(message(namespace,"form.item","第 {index} 项",{index:index + 1}))}
    size="small"
    style={{ marginBottom: 12 }}
    extra={hasToolbar ? <ArrayFieldItemButtonsTemplate {...buttonsProps} /> : undefined}
  >{children}</Card>;
}

function MultiSchemaFieldTemplate({ selector, optionSchemaField }: MultiSchemaFieldTemplateProps) {
  return <Space direction="vertical" size={12} style={{ width: "100%" }}>{selector}{optionSchemaField}</Space>;
}

function WrapIfAdditionalTemplate({ children, id, label, onKeyRename, onKeyRenameBlur, onRemoveProperty, disabled, readonly }: WrapIfAdditionalTemplateProps) {
  const i18n = usePortalI18n();
  if (!schemaIsAdditional(label)) return children;
  return <Space direction="vertical" size={8} style={{ width: "100%" }}>
    <Space style={{ width: "100%" }}>
      <Input defaultValue={label} disabled={disabled || readonly} aria-label={i18n.text(message(namespace,"form.propertyName","属性名称"))} onChange={onKeyRename} onBlur={onKeyRenameBlur} />
      <Button status="danger" icon={<IconDelete />} disabled={disabled || readonly} aria-label={i18n.text(message(namespace,"form.removeProperty","删除属性"))} onClick={onRemoveProperty} />
    </Space>
    <div id={id}>{children}</div>
  </Space>;
}

function schemaIsAdditional(label: string): boolean {
  return label !== "";
}

function ErrorListTemplate({ errors }: ErrorListProps) {
  const i18n = usePortalI18n();
  return errors.length === 0 ? null : <Alert type="error" title={i18n.text(message(namespace, "form.validationFailed", "表单校验未通过"))} content={i18n.text(message(namespace, "form.issueCount", "请检查 {count} 个问题", { count: errors.length }))} style={{ marginBottom: 16 }} />;
}

function IconButton({ icon, type, onClick, disabled, title, className, style, id, name, tabIndex, "aria-label": ariaLabel }: IconButtonProps) {
  return <Button
    id={id}
    name={name}
    className={className}
    style={style}
    tabIndex={tabIndex}
    htmlType={type}
    disabled={disabled}
    title={title}
    aria-label={ariaLabel}
    onClick={(event) => onClick?.(event as never)}
    icon={icon}
  />;
}

export const arcoFormTemplates: Partial<TemplatesType<FormData, Schema, FormContext>> = {
  BaseInputTemplate,
  FieldTemplate,
  ObjectFieldTemplate,
  ArrayFieldTemplate,
  ArrayFieldItemTemplate,
  ArrayFieldItemButtonsTemplate,
  MultiSchemaFieldTemplate,
  WrapIfAdditionalTemplate,
  ErrorListTemplate,
  ButtonTemplates: {
    SubmitButton: () => null,
    AddButton: (props) => <IconButton {...props} icon={<IconPlus />} />,
    CopyButton: (props) => <IconButton {...props} icon={<IconCopy />} />,
    MoveDownButton: (props) => <IconButton {...props} icon={<IconDown />} />,
    MoveUpButton: (props) => <IconButton {...props} icon={<IconUp />} />,
    RemoveButton: (props) => <IconButton {...props} icon={<IconDelete />} />,
    ClearButton: (props) => <IconButton {...props} icon={<IconDelete />} />,
  },
};

function pathFromError(error: RJSFValidationError): string {
  let path = error.property?.replace(/^\./, "") ?? "";
  if (error.name === "required" && typeof error.params?.missingProperty === "string") {
    path = path === "" ? error.params.missingProperty : `${path}.${error.params.missingProperty}`;
  }
  return path.replace(/\['([^']+)'\]/g, "$1");
}

function translatedMessage(error: RJSFValidationError, locale = "zh-CN"): string {
  const english = !locale.toLowerCase().startsWith("zh");
  switch (error.name) {
    case "required": return english ? "This field is required" : "此项为必填项";
    case "minLength": return english ? `Must contain at least ${String(error.params?.limit ?? "the required number of")} characters` : `至少需要 ${String(error.params?.limit ?? "指定")} 个字符`;
    case "maxLength": return english ? `Must contain no more than ${String(error.params?.limit ?? "the allowed number of")} characters` : `最多允许 ${String(error.params?.limit ?? "指定")} 个字符`;
    case "minimum": return english ? `Must not be less than ${String(error.params?.limit ?? "the minimum")}` : `不能小于 ${String(error.params?.limit ?? "最小值")}`;
    case "maximum": return english ? `Must not be greater than ${String(error.params?.limit ?? "the maximum")}` : `不能大于 ${String(error.params?.limit ?? "最大值")}`;
    case "minItems": return english ? `Must contain at least ${String(error.params?.limit ?? "the required number of")} items` : `至少需要 ${String(error.params?.limit ?? "指定")} 项`;
    case "maxItems": return english ? `Must contain no more than ${String(error.params?.limit ?? "the allowed number of")} items` : `最多允许 ${String(error.params?.limit ?? "指定")} 项`;
    case "pattern": return english ? "The value does not match the required format" : "格式不符合要求";
    case "format": return english ? "The value has an invalid format" : "格式不正确";
    case "additionalProperties": return english ? "Contains a property that is not allowed" : "包含未允许的属性";
    default: return error.message ?? (english ? "Value does not match the schema" : "值不符合 Schema");
  }
}

export function transformArcoFormErrors(errors: RJSFValidationError[]): RJSFValidationError[] {
  return transformLocalizedFormErrors(errors, "zh-CN");
}

function transformLocalizedFormErrors(errors: RJSFValidationError[], locale: string): RJSFValidationError[] {
  return errors.map((error) => ({ ...error, message: translatedMessage(error, locale), stack: `${pathFromError(error)} ${translatedMessage(error, locale)}`.trim() }));
}

function errorsByPath(errors: RJSFValidationError[], locale: string): Record<string, string> {
  const result: Record<string, string> = {};
  for (const error of errors) result[pathFromError(error) || "$form"] ??= translatedMessage(error, locale);
  return result;
}

function issuesFromErrors(errors: RJSFValidationError[], locale: string): FormValidationIssue[] {
  return errors.map((error) => ({
    path: pathFromError(error),
    code: error.name ?? "schema",
    message: translatedMessage(error, locale),
    ...(error.schemaPath === undefined ? {} : { schemaPath: error.schemaPath }),
  }));
}

function errorSchemaFromPaths(errors: Readonly<Record<string, string>>): ErrorSchema<FormData> {
  const root: Record<string, unknown> = {};
  for (const [path, message] of Object.entries(errors)) {
    const parts = path === "$form" ? [] : path.replace(/\[(\d+)\]/g, ".$1").split(".").filter(Boolean);
    let node = root;
    for (const part of parts) {
      const child = node[part];
      if (typeof child !== "object" || child === null || Array.isArray(child)) node[part] = {};
      node = node[part] as Record<string, unknown>;
    }
    const messages = Array.isArray(node.__errors) ? node.__errors as string[] : [];
    node.__errors = [...messages, message];
  }
  return root as ErrorSchema<FormData>;
}

function schemaContractError(schema: FormRendererProps["schema"], english: boolean): string | undefined {
  if (schema.schema.$schema !== jsonSchemaDialect) return english ? `Only JSON Schema Draft 7 is supported: ${jsonSchemaDialect}` : `仅支持 JSON Schema Draft 7：${jsonSchemaDialect}`;
  if (schema.schema.type !== "object") return english ? "The form root schema type must be object" : "表单根 Schema 的 type 必须是 object";
  if (typeof schema.schema.properties !== "object" || schema.schema.properties === null || Array.isArray(schema.schema.properties)) return english ? "The form root schema must declare properties" : "表单根 Schema 必须声明 properties";
  return undefined;
}

export function ArcoJSONSchemaForm({ schema, value, onChange, presentation, presentationSection, onPresentationSectionChange, readOnly, submitting, errors: externalErrors, context: suppliedContext, validate, validationDelayMs = 250, onValidationChange }: FormRendererProps) {
  const i18n = usePortalI18n();
  const contractError = schemaContractError(schema,!i18n.locale.toLowerCase().startsWith("zh"));
  const validationSchema = schema.schema as Schema;
  const dataSchema = useMemo(() => localizeJSONSchema(schema.schema, schema.localization, i18n.text) as Schema, [i18n.text, schema.localization, schema.schema]);
  const uiSchema = useMemo(() => schema.uiSchema === undefined ? undefined : localizeJSONSchema(schema.uiSchema, schema.uiLocalization, i18n.text) as UiSchema<FormData, Schema, FormContext>, [i18n.text, schema.uiLocalization, schema.uiSchema]);
  const context = suppliedContext ?? emptyContext;
  const syncValidation = useMemo(() => {
    if (contractError !== undefined) return { errors: [{ name: "schema", property: "", schemaPath: "", stack: contractError, message: contractError }] as RJSFValidationError[] };
    try {
      return arcoJSONSchemaValidator.validateFormData(value, validationSchema, undefined, (errors) => transformLocalizedFormErrors(errors, i18n.locale), uiSchema);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Schema 编译失败";
      return { errors: [{ name: "schema", property: "", schemaPath: "", stack: message, message }] as RJSFValidationError[] };
    }
  }, [contractError, i18n.locale, uiSchema, validationSchema, value]);
  const syncErrors = useMemo(() => errorsByPath(syncValidation.errors, i18n.locale), [i18n.locale, syncValidation.errors]);
  const [asyncValidation, setAsyncValidation] = useState<{
    source?: Readonly<FormData>;
    validating: boolean;
    errors: Readonly<Record<string, string>>;
  }>({ validating: false, errors: {} });
  const currentAsync = asyncValidation.source === value
    ? asyncValidation
    : { source: value, validating: validate !== undefined && syncValidation.errors.length === 0, errors: {} };

  useEffect(() => {
    if (validate === undefined || syncValidation.errors.length > 0) {
      setAsyncValidation({ source: value, validating: false, errors: {} });
      return;
    }
    const controller = new AbortController();
    setAsyncValidation({ source: value, validating: true, errors: {} });
    const timeout = window.setTimeout(() => {
      validate({ schema, value, context, signal: controller.signal })
        .then((errors) => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors }); })
        .catch(() => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors: { $form: i18n.text(message(namespace,"form.asyncUnavailable","异步校验暂时不可用")) } }); });
    }, Math.max(0, validationDelayMs));
    return () => { controller.abort(); window.clearTimeout(timeout); };
  }, [context, schema, syncValidation.errors.length, validate, validationDelayMs, value]);

  const allExternalErrors = useMemo(() => ({ ...currentAsync.errors, ...externalErrors }), [currentAsync.errors, externalErrors]);
  const errors = useMemo(() => ({ ...syncErrors, ...allExternalErrors }), [allExternalErrors, syncErrors]);
  const validationState = useMemo(() => ({
    valid: syncValidation.errors.length === 0 && !currentAsync.validating && Object.keys(allExternalErrors).length === 0,
    issues: issuesFromErrors(syncValidation.errors, i18n.locale),
    errors,
    validating: currentAsync.validating,
  }), [allExternalErrors, currentAsync.validating, errors, i18n.locale, syncValidation.errors]);
  useEffect(() => onValidationChange?.(validationState), [onValidationChange, validationState]);
  const templates = useMemo(() => ({ ...arcoFormTemplates, ObjectFieldTemplate: (props: ObjectFieldTemplateProps<FormData, Schema, FormContext>) => <PresentedObjectFieldTemplate {...props} presentation={presentation} activeSection={presentationSection} onSectionChange={onPresentationSectionChange} /> }), [onPresentationSectionChange, presentation, presentationSection]);

  if (contractError !== undefined) return <Alert type="error" title={i18n.text(message(namespace,"form.unsupported","表单 Schema 不受支持"))} content={contractError} />;
  return <>
    {currentAsync.validating ? <Alert type="info" title={i18n.text(message(namespace,"form.validating","正在校验"))} style={{ marginBottom: 16 }} /> : null}
    <Form layout={presentation?.layout === "horizontal" ? "horizontal" : "vertical"} size={presentation?.layout === "compact" ? "small" : undefined}>
      <RJSFForm<FormData, Schema, FormContext>
        tagName="div"
        schema={dataSchema}
        uiSchema={uiSchema}
        formData={value}
        formContext={context}
        validator={arcoJSONSchemaValidator}
        widgets={arcoFormWidgets}
        templates={templates}
        readonly={readOnly}
        disabled={submitting}
        liveValidate="onChange"
        showErrorList="top"
        extraErrors={errorSchemaFromPaths(allExternalErrors)}
        extraErrorsBlockSubmit
        noHtml5Validate
        transformErrors={(items) => transformLocalizedFormErrors(items, i18n.locale)}
        onChange={(event) => onChange(event.formData ?? {})}
      ><></></RJSFForm>
    </Form>
  </>;
}
