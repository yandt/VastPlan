import { ConfigProvider, Input, Select as AntdSelect, Steps, Tabs, Typography } from "antd";
import RJSFForm from "@rjsf/core/lib/components/Form.js";
import { generateWidgets } from "@rjsf/antd/lib/widgets/index.js";
import { enumOptionsIndexForValue, enumOptionsValueForIndex } from "@rjsf/utils";
import type { FieldTemplateProps, ObjectFieldTemplateProps, RJSFValidationError, WidgetProps } from "@rjsf/utils";
import { useEffect, useMemo, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { cspJSONSchemaValidator } from "@vastplan/rjsf-csp-validator";
import type { FormPresentation, FormRendererProps, FormSectionPresentation, LocalizedText } from "@vastplan/ui-primitives";
import { componentSizeRecipes, formControlAlignment, formGridClassName, formGridColumns, formGridCSS, formGridStyle, formLabelPlacement, localizeJSONSchema, message, useComponentSize, usePortalI18n } from "@vastplan/ui-primitives";
import { namespace } from "./theme";
import { safeAntdTemplates } from "./safe-rjsf-theme";
import { PresentedField, antdFormFieldWidthCSS, antdInsideInlineCSS } from "./inside-inline-field";
import { antdComponentSize } from "./component-size";
import { resolveFormLabelWidth } from "./form-label-width";
import { DurationWidget, antdDurationWidgetCSS } from "./duration-widget";

const validator = cspJSONSchemaValidator;
const emptyFormContext: Readonly<Record<string, unknown>> = Object.freeze({});
const antdWidgets = generateWidgets();
const compactFormTheme = { components: { Form: { itemMarginBottom: 0 } } };
const controlAlignmentCSS = `
.vp-antd-form-controls-start,.vp-antd-form-controls-end,.vp-antd-form-controls-start>.rjsf,.vp-antd-form-controls-end>.rjsf{width:100%;min-width:0}
.vp-antd-form-object{width:100%;min-width:0}
.vp-antd-form-controls-start .ant-form-item-control-input-content,.vp-antd-form-controls-end .ant-form-item-control-input-content{display:flex;min-width:0}
.vp-antd-form-controls-start .ant-form-item-control-input-content{justify-content:flex-start}
.vp-antd-form-controls-end .ant-form-item-control-input-content{justify-content:flex-end}
.vp-antd-form-controls-start .ant-form-item-control-input-content>*,.vp-antd-form-controls-end .ant-form-item-control-input-content>*{max-width:100%}
.vp-antd-form-section{display:grid;gap:var(--vp-form-grid-gap,12px);min-width:0}
.vp-antd-form-section-heading{display:flex;align-items:center;gap:8px;min-width:0;line-height:1.4}
.vp-antd-form-section-heading>span[aria-hidden="true"]{display:block;flex:1 1 auto;min-width:24px;height:1px;background:var(--ant-color-split,#f0f0f0)}
.vp-antd-form-section-collapsible>summary{cursor:pointer;font-weight:600}
`;

type TextResolver = (value: LocalizedText) => string;

/** Translate normalized JSON Schema errors at the Renderer boundary; validator messages are diagnostic-only. */
export function localizeValidationErrors(errors: RJSFValidationError[], text: TextResolver): RJSFValidationError[] {
  return errors.map((error) => {
    const descriptor = validationMessage(error);
    const localized = text(descriptor);
    return { ...error, message: localized, stack: `${error.property ?? ""} ${localized}`.trim() };
  });
}

function validationMessage(error: RJSFValidationError): LocalizedText {
  const limit = validationLimit(error);
  const name = error.name ?? "invalid";
  switch (name) {
    case "required": return message(namespace, "form.validation.required", "此项为必填项");
    case "minLength": return message(namespace, "form.validation.minLength", "请至少输入 {limit} 个字符", { limit });
    case "maxLength": return message(namespace, "form.validation.maxLength", "最多可输入 {limit} 个字符", { limit });
    case "minimum": return message(namespace, "form.validation.minimum", "数值不能小于 {limit}", { limit });
    case "maximum": return message(namespace, "form.validation.maximum", "数值不能大于 {limit}", { limit });
    case "exclusiveMinimum": return message(namespace, "form.validation.exclusiveMinimum", "数值必须大于 {limit}", { limit });
    case "exclusiveMaximum": return message(namespace, "form.validation.exclusiveMaximum", "数值必须小于 {limit}", { limit });
    case "minItems": return message(namespace, "form.validation.minItems", "请至少添加 {limit} 项", { limit });
    case "maxItems": return message(namespace, "form.validation.maxItems", "最多可添加 {limit} 项", { limit });
    case "minProperties": return message(namespace, "form.validation.minProperties", "请至少填写 {limit} 项", { limit });
    case "maxProperties": return message(namespace, "form.validation.maxProperties", "最多可填写 {limit} 项", { limit });
    case "pattern": return message(namespace, "form.validation.pattern", "输入格式不正确");
    case "enum": return message(namespace, "form.validation.enum", "请选择有效选项");
    case "type": return message(namespace, "form.validation.type", "输入值类型不正确");
    case "uniqueItems": return message(namespace, "form.validation.uniqueItems", "列表中不能包含重复项");
    case "additionalProperties": return message(namespace, "form.validation.additionalProperties", "包含不允许的字段");
    default:
      return name === "format" || name.startsWith("format")
        ? message(namespace, "form.validation.format", "输入格式不正确")
        : message(namespace, "form.invalid", "输入内容不符合要求");
  }
}

function validationLimit(error: RJSFValidationError): string | number {
  const value = error.params?.limit;
  return typeof value === "string" || typeof value === "number" ? value : "?";
}

function SecretRefWidget({ value, disabled, readonly, required, onChange, onBlur, onFocus, id, label }: WidgetProps) {
  const i18n = usePortalI18n();
  return <Input
    id={id}
    value={typeof value === "string" ? value : ""}
    disabled={disabled}
    readOnly={readonly}
    required={required}
    aria-label={label}
    autoComplete="off"
    placeholder={i18n.text(message(namespace, "form.credentialPlaceholder", "输入 credential:// 凭证引用（禁止填写明文）"))}
    onChange={(event) => onChange(event.target.value)}
    onBlur={(event) => onBlur(id, event.target.value)}
    onFocus={(event) => onFocus(id, event.target.value)}
  />;
}

/** FilterPanel opts into this through ui:options.allowClear; regular form selects retain their current behavior. */
function SelectWidget({ id, value, multiple, placeholder, disabled, readonly, options, onChange, onBlur, onFocus }: WidgetProps) {
  const enumOptions = options.enumOptions ?? [];
  const selected = enumOptionsIndexForValue(value, enumOptions, multiple);
  return <AntdSelect
    id={id}
    value={selected}
    mode={multiple ? "multiple" : undefined}
    placeholder={placeholder}
    disabled={disabled || readonly}
    allowClear={options.allowClear === true}
    style={{ width: "100%" }}
    options={enumOptions.map((option, index) => ({
      value: String(index), label: option.label,
      disabled: Array.isArray(options.enumDisabled) && options.enumDisabled.includes(option.value),
    }))}
    onChange={(next) => onChange(next === undefined ? options.emptyValue : enumOptionsValueForIndex(next, enumOptions, options.emptyValue))}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  />;
}

type PresentedObjectProps = ObjectFieldTemplateProps & { presentation?: FormPresentation; activeSection?: string; onSectionChange?(id: string): void };

function PresentedObject({ presentation, activeSection, onSectionChange, ...props }: PresentedObjectProps) {
  const i18n = usePortalI18n();
  const size = useComponentSize();
  const compactRoot = props.fieldPathId.path.length === 0 && presentation?.layout === "compact";
  if (props.fieldPathId.path.length !== 0) {
    const rootField = String(props.fieldPathId.path[0] ?? "");
    const directPointer = formPropertyPointer([], rootField);
    const owningSection = presentation?.navigation === "sections" && props.fieldPathId.path.length === 1
      ? presentation.sections?.find((section) => section.fields.includes(directPointer))
      : undefined;
    const columns = owningSection === undefined ? 1 : formGridColumns(presentation, owningSection);
    const properties = props.properties.filter((property) => !property.hidden).map((property) => {
      const pointer = formPropertyPointer(props.fieldPathId.path, property.name);
      const span = formFieldSpan(presentation, pointer, columns);
      return <div key={property.name} style={owningSection === undefined ? undefined : { gridColumn: `span ${span}` }}>{property.content}</div>;
    });
    return <section className="vp-antd-form-object">{owningSection !== undefined || props.title === "" ? null : <Typography.Title level={5}>{props.title}</Typography.Title>}{props.description}{owningSection === undefined ? properties : <div className={formGridClassName} style={formGridStyle(presentation, owningSection)}>{properties}</div>}</section>;
  }
  if (presentation?.sections === undefined || presentation.sections.length === 0) {
    const columns = formGridColumns(presentation);
    return <section className="vp-antd-form-object">{compactRoot || props.title === "" ? null : <Typography.Title level={5}>{props.title}</Typography.Title>}{props.description}<div className={formGridClassName} style={formGridStyle(presentation)}>{props.properties.filter((property) => !property.hidden).map((property) => {
      const span = formFieldSpan(presentation, formPropertyPointer([], property.name), columns);
      return <div key={property.name} style={{ gridColumn: `span ${span}` }}>{property.content}</div>;
    })}</div></section>;
  }
  const sections = presentation.sections;
  const selected = sections.find((section) => section.id === activeSection) ?? sections[0]!;
  const assigned = new Set(sections.flatMap((section) => section.fields.map(formFieldName)));
  const remainder = props.properties.filter((property) => !assigned.has(property.name) && !property.hidden);
  const renderSection = (section: FormSectionPresentation) => {
    const fields = section.fields.map(formFieldName);
    const columns = formGridColumns(presentation, section);
    const body = <div className={formGridClassName} style={formGridStyle(presentation, section)}>{props.properties.filter((property) => fields.includes(property.name) && !property.hidden).map((property) => {
      const span = formFieldSpan(presentation, formPropertyPointer([], property.name), columns);
      return <div key={property.name} style={{ gridColumn: `span ${span}` }}>{property.content}</div>;
    })}</div>;
    const description = section.description === undefined ? null : <Typography.Paragraph type="secondary" style={{ marginBlock: 0 }}>{i18n.text(section.description)}</Typography.Paragraph>;
    if (presentation.navigation !== "sections") return <>{description}{body}</>;
    const title = section.title === undefined ? undefined : i18n.text(section.title);
    return section.collapsible
      ? <details className="vp-antd-form-section vp-antd-form-section-collapsible"><summary>{title ?? section.id}</summary>{description}{body}</details>
      : <section className="vp-antd-form-section">{title === undefined ? null : <div className="vp-antd-form-section-heading"><Typography.Text strong>{title}</Typography.Text><span aria-hidden="true" /></div>}{description}{body}</section>;
  };
  const remaining = remainder.length === 0 ? null : <div>{remainder.map((property) => <div key={property.name}>{property.content}</div>)}</div>;
  if (presentation.navigation === "tabs") return <><Tabs activeKey={selected.id} onChange={onSectionChange} items={sections.map((section) => ({ key: section.id, label: section.title === undefined ? section.id : i18n.text(section.title), children: renderSection(section) }))} />{remaining}</>;
  if (presentation.navigation === "steps") {
    const current = Math.max(0, sections.findIndex((section) => section.id === selected.id));
    return <><Steps current={current} onChange={(index) => onSectionChange?.(sections[index]!.id)} items={sections.map((section, index) => ({ title: section.title === undefined ? `${index + 1}` : i18n.text(section.title) }))} /><div style={{ marginTop: 24 }}>{renderSection(selected)}</div>{remaining}</>;
  }
  return <div className="vp-antd-form-object vp-antd-form-sections" style={{ display: "grid", gap: `var(--vp-form-section-gap, ${componentSizeRecipes.layout[size].gap}px)` }}>{sections.map((section) => <div key={section.id}>{renderSection(section)}</div>)}{remaining}</div>;
}

export function FormRenderer({ schema, value, onChange, size: requestedSize, presentation, presentationSection, onPresentationSectionChange, readOnly, submitting, errors: externalErrors = {}, context: suppliedContext, validate, validationDelayMs = 250, onValidationChange }: FormRendererProps) {
  const size = useComponentSize(requestedSize);
  const i18n = usePortalI18n();
  const formContext = suppliedContext ?? emptyFormContext;
  const localizedSchema = useMemo(() => localizeJSONSchema(schema.schema, schema.localization, i18n.text), [i18n.text, schema.localization, schema.schema]);
  const localizedUISchema = useMemo(() => schema.uiSchema === undefined ? undefined : localizeJSONSchema(schema.uiSchema, schema.uiLocalization, i18n.text), [i18n.text, schema.uiLocalization, schema.uiSchema]);
  const transformErrors = useMemo(() => (errors: RJSFValidationError[]) => localizeValidationErrors(errors, i18n.text), [i18n.text]);
  const validation = useMemo(() => validator.validateFormData(value, schema.schema, undefined, transformErrors, schema.uiSchema), [schema.schema, schema.uiSchema, transformErrors, value]);
  const syncErrors = useMemo(() => Object.fromEntries(validation.errors.map((error, index) => [errorPath(error) || `form.${index}`, error.message ?? i18n.text(message(namespace, "form.invalid", "输入内容不符合要求"))])), [i18n, validation.errors]);
  const [asyncValidation, setAsyncValidation] = useState<{ source?: Readonly<Record<string, unknown>>; validating: boolean; errors: Readonly<Record<string, string>> }>({ validating: false, errors: {} });
  const currentAsync = asyncValidation.source === value ? asyncValidation : { source: value, validating: validate !== undefined && validation.errors.length === 0, errors: {} };
  useEffect(() => {
    if (validate === undefined || validation.errors.length > 0) { setAsyncValidation({ source: value, validating: false, errors: {} }); return; }
    const controller = new AbortController();
    setAsyncValidation({ source: value, validating: true, errors: {} });
    const timeout = window.setTimeout(() => {
      validate({ schema, value, context: formContext, signal: controller.signal })
        .then((errors) => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors }); })
        .catch(() => { if (!controller.signal.aborted) setAsyncValidation({ source: value, validating: false, errors: { $form: i18n.text(message(namespace, "form.asyncUnavailable", "异步校验暂时不可用")) } }); });
    }, Math.max(0, validationDelayMs));
    return () => { controller.abort(); window.clearTimeout(timeout); };
  }, [formContext, i18n, schema, validate, validation.errors.length, validationDelayMs, value]);
  const combinedExternalErrors = useMemo(() => ({ ...currentAsync.errors, ...externalErrors }), [currentAsync.errors, externalErrors]);
  useEffect(() => {
    onValidationChange?.({
      valid: validation.errors.length === 0 && !currentAsync.validating && Object.keys(combinedExternalErrors).length === 0,
      issues: validation.errors.map((error) => ({ path: errorPath(error), code: error.name ?? "schema_invalid", message: error.message, schemaPath: error.schemaPath })),
      errors: { ...syncErrors, ...combinedExternalErrors },
      validating: currentAsync.validating,
    });
  }, [combinedExternalErrors, currentAsync.validating, onValidationChange, syncErrors, validation.errors]);
  const templates = useMemo(() => ({ ...safeAntdTemplates, FieldTemplate: (props: FieldTemplateProps) => <PresentedField {...props} placement={formLabelPlacement(presentation)} />, ObjectFieldTemplate: (props: ObjectFieldTemplateProps) => <PresentedObject {...props} presentation={presentation} activeSection={presentationSection} onSectionChange={onPresentationSectionChange} />, ButtonTemplates: { ...safeAntdTemplates.ButtonTemplates, SubmitButton: () => null } }), [onPresentationSectionChange, presentation, presentationSection]);
  const widgets = useMemo(() => ({ ...antdWidgets, SelectWidget, duration: DurationWidget, secretRef: SecretRefWidget }), []);
  const compact = presentation?.layout === "compact";
  const controlAlignment = formControlAlignment(presentation);
  const form = <RJSFForm
    schema={localizedSchema}
    uiSchema={localizedUISchema}
    formData={value}
    validator={validator}
    readonly={readOnly}
    disabled={submitting}
    liveValidate="onChange"
    showErrorList={false}
    extraErrors={errorSchema(combinedExternalErrors) as never}
    extraErrorsBlockSubmit
    noHtml5Validate
    transformErrors={transformErrors}
    formContext={formContext}
    onChange={(event) => onChange((event.formData ?? {}) as Record<string, unknown>)}
    templates={templates}
    widgets={widgets}
  ><></></RJSFForm>;
  const rhythmStyle = {
    "--vp-form-grid-gap": `var(--vp-form-dialog-row-gap, ${componentSizeRecipes.layout[size].gap}px)`,
    "--vp-form-section-gap": `var(--vp-form-dialog-section-gap, ${componentSizeRecipes.layout[size].gap}px)`,
    "--vp-form-item-margin-bottom": "var(--vp-form-dialog-item-margin, 16px)",
    "--vp-form-label-width": `${resolveFormLabelWidth(localizedSchema, size)}px`,
    margin: componentSizeRecipes.layout[size].outerMargin,
  } as CSSProperties;
  return <ConfigProvider componentSize={antdComponentSize[size]} theme={compact ? compactFormTheme : undefined}>
    <style>{formGridCSS}{controlAlignmentCSS}{antdFormFieldWidthCSS}{antdDurationWidgetCSS}{formLabelPlacement(presentation) === "inside-inline" ? antdInsideInlineCSS : ""}</style>
    <div className={`vp-antd-form-controls-${controlAlignment}`} data-form-control-alignment={controlAlignment} style={rhythmStyle}>{form}</div>
  </ConfigProvider>;
}


function formFieldName(pointer: string): string {
  const first = pointer.startsWith("/") ? pointer.slice(1).split("/")[0] ?? "" : pointer;
  return first.replace(/~1/g, "/").replace(/~0/g, "~");
}

function formPropertyPointer(path: readonly (string | number)[], property: string): string {
  return `/${[...path, property].map((part) => String(part).replace(/~/g, "~0").replace(/\//g, "~1")).join("/")}`;
}

function formFieldSpan(presentation: FormPresentation | undefined, pointer: string, columns: number): number {
  const span = presentation?.fields?.find((field) => field.pointer === pointer)?.span ?? 1;
  return Math.min(Math.max(1, span), columns);
}

function errorPath(error: { property?: string; name?: string; params?: { missingProperty?: unknown } }): string {
  let path = error.property?.replace(/^\./, "") ?? "";
  if (error.name === "required" && typeof error.params?.missingProperty === "string") path = path === "" ? error.params.missingProperty : `${path}.${error.params.missingProperty}`;
  return path.replace(/\['([^']+)'\]/g, "$1");
}

function errorSchema(errors: Readonly<Record<string, string>>): Record<string, unknown> {
  const root: Record<string, unknown> = {};
  for (const [path, value] of Object.entries(errors)) {
    const parts = path === "$form" ? [] : path.replace(/\[(\d+)\]/g, ".$1").split(".").filter(Boolean);
    let node = root;
    for (const part of parts) {
      if (typeof node[part] !== "object" || node[part] === null || Array.isArray(node[part])) node[part] = {};
      node = node[part] as Record<string, unknown>;
    }
    node.__errors = [...(Array.isArray(node.__errors) ? node.__errors as string[] : []), value];
  }
  return root;
}
