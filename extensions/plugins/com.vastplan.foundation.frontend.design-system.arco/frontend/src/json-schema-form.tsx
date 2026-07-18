import {
  Alert,
  Button,
  Card,
  ColorPicker,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Rate,
  Select,
  Space,
  Switch,
  TimePicker,
  Typography,
  IconCopy,
  IconDelete,
  IconDown,
  IconPlus,
  IconUp,
} from "./arco-components";
import RJSFForm from "@rjsf/core";
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
import { customizeValidator } from "@rjsf/validator-ajv8";
import { useEffect, useMemo, useState } from "react";
import type { FormRendererProps, FormValidationIssue } from "@vastplan/portal-ui";
import { jsonSchemaDialect } from "@vastplan/portal-ui";

type FormData = Record<string, unknown>;
type FormContext = Readonly<Record<string, unknown>>;
type Schema = RJSFSchema;

const emptyContext: FormContext = {};

export const arcoJSONSchemaValidator = customizeValidator<FormData, Schema, FormContext>({
  customFormats: {
    "vastplan-credential-ref": /^credential:\/\/[A-Za-z0-9][A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*$/,
  },
  ajvOptionsOverrides: {
    allErrors: true,
    strict: false,
    loadSchema: undefined,
  },
});

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
  return <Alert type="warning" content="表单不接受内嵌文件数据；请使用受控制品上传能力。" />;
}

function SecretRefWidget(props: WidgetProps) {
  if ((props.options.enumOptions?.length ?? 0) > 0) return <SelectWidget {...props} />;
  return <Input
    id={props.id}
    value={typeof props.value === "string" ? props.value : ""}
    placeholder={props.placeholder ?? "输入 credential:// 凭证引用（禁止填写明文）"}
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
  PasswordWidget: BaseInputTemplate,
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
  const content = <>{properties.filter((property) => !property.hidden).map((property) => property.content)}</>;
  const add = canExpand(schema, uiSchema, formData) && !readonly && !disabled
    ? <Button size="small" icon={<IconPlus />} onClick={onAddProperty}>添加属性</Button>
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

function ArrayFieldTemplate({ title, items, canAdd, onAddClick, readonly, disabled, rawErrors }: ArrayFieldTemplateProps) {
  return <Card
    title={title}
    size="small"
    style={{ marginBottom: 16 }}
    extra={canAdd && !readonly && !disabled ? <Button size="small" icon={<IconPlus />} onClick={onAddClick}>添加</Button> : undefined}
  >
    {(rawErrors?.length ?? 0) > 0 ? <Alert type="error" content={rawErrors?.[0]} style={{ marginBottom: 12 }} /> : null}
    {items.length === 0 ? <Typography.Text type="secondary">暂无条目</Typography.Text> : items}
  </Card>;
}

function ArrayFieldItemButtonsTemplate({ hasMoveUp, hasMoveDown, hasCopy, hasRemove, disabled, readonly, onMoveUpItem, onMoveDownItem, onCopyItem, onRemoveItem }: ArrayFieldItemButtonsTemplateProps) {
  const locked = Boolean(disabled || readonly);
  return <Space size={4}>
    {hasMoveUp ? <Button size="mini" icon={<IconUp />} disabled={locked} aria-label="上移" onClick={onMoveUpItem} /> : null}
    {hasMoveDown ? <Button size="mini" icon={<IconDown />} disabled={locked} aria-label="下移" onClick={onMoveDownItem} /> : null}
    {hasCopy ? <Button size="mini" icon={<IconCopy />} disabled={locked} aria-label="复制" onClick={onCopyItem} /> : null}
    {hasRemove ? <Button size="mini" status="danger" icon={<IconDelete />} disabled={locked} aria-label="删除" onClick={onRemoveItem} /> : null}
  </Space>;
}

function ArrayFieldItemTemplate({ children, buttonsProps, index, hasToolbar }: ArrayFieldItemTemplateProps) {
  return <Card
    key={buttonsProps.fieldPathId.$id}
    title={`第 ${index + 1} 项`}
    size="small"
    style={{ marginBottom: 12 }}
    extra={hasToolbar ? <ArrayFieldItemButtonsTemplate {...buttonsProps} /> : undefined}
  >{children}</Card>;
}

function MultiSchemaFieldTemplate({ selector, optionSchemaField }: MultiSchemaFieldTemplateProps) {
  return <Space direction="vertical" size={12} style={{ width: "100%" }}>{selector}{optionSchemaField}</Space>;
}

function WrapIfAdditionalTemplate({ children, id, label, onKeyRename, onKeyRenameBlur, onRemoveProperty, disabled, readonly }: WrapIfAdditionalTemplateProps) {
  if (!schemaIsAdditional(label)) return children;
  return <Space direction="vertical" size={8} style={{ width: "100%" }}>
    <Space style={{ width: "100%" }}>
      <Input defaultValue={label} disabled={disabled || readonly} aria-label="属性名称" onChange={onKeyRename} onBlur={onKeyRenameBlur} />
      <Button status="danger" icon={<IconDelete />} disabled={disabled || readonly} aria-label="删除属性" onClick={onRemoveProperty} />
    </Space>
    <div id={id}>{children}</div>
  </Space>;
}

function schemaIsAdditional(label: string): boolean {
  return label !== "";
}

function ErrorListTemplate({ errors }: ErrorListProps) {
  return errors.length === 0 ? null : <Alert type="error" title="表单校验未通过" content={`请检查 ${errors.length} 个问题`} style={{ marginBottom: 16 }} />;
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

function translatedMessage(error: RJSFValidationError): string {
  switch (error.name) {
    case "required": return "此项为必填项";
    case "minLength": return `至少需要 ${String(error.params?.limit ?? "指定")} 个字符`;
    case "maxLength": return `最多允许 ${String(error.params?.limit ?? "指定")} 个字符`;
    case "minimum": return `不能小于 ${String(error.params?.limit ?? "最小值")}`;
    case "maximum": return `不能大于 ${String(error.params?.limit ?? "最大值")}`;
    case "minItems": return `至少需要 ${String(error.params?.limit ?? "指定")} 项`;
    case "maxItems": return `最多允许 ${String(error.params?.limit ?? "指定")} 项`;
    case "pattern": return "格式不符合要求";
    case "format": return "格式不正确";
    case "additionalProperties": return "包含未允许的属性";
    default: return error.message ?? "值不符合 Schema";
  }
}

export function transformArcoFormErrors(errors: RJSFValidationError[]): RJSFValidationError[] {
  return errors.map((error) => ({ ...error, message: translatedMessage(error), stack: `${pathFromError(error)} ${translatedMessage(error)}`.trim() }));
}

function errorsByPath(errors: RJSFValidationError[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const error of errors) result[pathFromError(error) || "$form"] ??= translatedMessage(error);
  return result;
}

function issuesFromErrors(errors: RJSFValidationError[]): FormValidationIssue[] {
  return errors.map((error) => ({
    path: pathFromError(error),
    code: error.name ?? "schema",
    message: translatedMessage(error),
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

function schemaContractError(schema: FormRendererProps["schema"]): string | undefined {
  if (schema.schema.$schema !== jsonSchemaDialect) return `仅支持 JSON Schema Draft 7：${jsonSchemaDialect}`;
  if (schema.schema.type !== "object") return "表单根 Schema 的 type 必须是 object";
  if (typeof schema.schema.properties !== "object" || schema.schema.properties === null || Array.isArray(schema.schema.properties)) return "表单根 Schema 必须声明 properties";
  return undefined;
}

export function ArcoJSONSchemaForm({ schema, value, onChange, readOnly, submitting, errors: externalErrors, context: suppliedContext, validate, validationDelayMs = 250, onValidationChange }: FormRendererProps) {
  const contractError = schemaContractError(schema);
  const dataSchema = schema.schema as Schema;
  const uiSchema = schema.uiSchema as UiSchema<FormData, Schema, FormContext> | undefined;
  const context = suppliedContext ?? emptyContext;
  const syncValidation = useMemo(() => {
    if (contractError !== undefined) return { errors: [{ name: "schema", property: "", schemaPath: "", stack: contractError, message: contractError }] as RJSFValidationError[] };
    try {
      return arcoJSONSchemaValidator.validateFormData(value, dataSchema, undefined, transformArcoFormErrors, uiSchema);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Schema 编译失败";
      return { errors: [{ name: "schema", property: "", schemaPath: "", stack: message, message }] as RJSFValidationError[] };
    }
  }, [contractError, dataSchema, uiSchema, value]);
  const syncErrors = useMemo(() => errorsByPath(syncValidation.errors), [syncValidation.errors]);
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
        .catch(() => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors: { $form: "异步校验暂时不可用" } }); });
    }, Math.max(0, validationDelayMs));
    return () => { controller.abort(); window.clearTimeout(timeout); };
  }, [context, schema, syncValidation.errors.length, validate, validationDelayMs, value]);

  const allExternalErrors = useMemo(() => ({ ...currentAsync.errors, ...externalErrors }), [currentAsync.errors, externalErrors]);
  const errors = useMemo(() => ({ ...syncErrors, ...allExternalErrors }), [allExternalErrors, syncErrors]);
  const validationState = useMemo(() => ({
    valid: syncValidation.errors.length === 0 && !currentAsync.validating && Object.keys(allExternalErrors).length === 0,
    issues: issuesFromErrors(syncValidation.errors),
    errors,
    validating: currentAsync.validating,
  }), [allExternalErrors, currentAsync.validating, errors, syncValidation.errors]);
  useEffect(() => onValidationChange?.(validationState), [onValidationChange, validationState]);

  if (contractError !== undefined) return <Alert type="error" title="表单 Schema 不受支持" content={contractError} />;
  return <>
    {currentAsync.validating ? <Alert type="info" title="正在校验" style={{ marginBottom: 16 }} /> : null}
    <Form layout="vertical">
      <RJSFForm<FormData, Schema, FormContext>
        tagName="div"
        schema={dataSchema}
        uiSchema={uiSchema}
        formData={value}
        formContext={context}
        validator={arcoJSONSchemaValidator}
        widgets={arcoFormWidgets}
        templates={arcoFormTemplates}
        readonly={readOnly}
        disabled={submitting}
        liveValidate="onChange"
        showErrorList="top"
        extraErrors={errorSchemaFromPaths(allExternalErrors)}
        extraErrorsBlockSubmit
        noHtml5Validate
        transformErrors={transformArcoFormErrors}
        onChange={(event) => onChange(event.formData ?? {})}
      ><></></RJSFForm>
    </Form>
  </>;
}
