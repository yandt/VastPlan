import { Card, ConfigProvider, Input, Steps, Tabs, Typography } from "antd";
import RJSFForm from "@rjsf/core/lib/components/Form.js";
import { generateWidgets } from "@rjsf/antd/lib/widgets/index.js";
import type { ObjectFieldTemplateProps, WidgetProps } from "@rjsf/utils";
import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { cspJSONSchemaValidator } from "@vastplan/rjsf-csp-validator";
import type { FormPresentation, FormRendererProps, FormSectionPresentation } from "@vastplan/ui-primitives";
import { localizeJSONSchema, message, usePortalI18n } from "@vastplan/ui-primitives";
import { namespace } from "./theme";
import { safeAntdTemplates } from "./safe-rjsf-theme";

const validator = cspJSONSchemaValidator;
const emptyFormContext: Readonly<Record<string, unknown>> = Object.freeze({});
const antdWidgets = generateWidgets();
const compactFormTheme = { components: { Form: { itemMarginBottom: 0 } } };

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

type PresentedObjectProps = ObjectFieldTemplateProps & { presentation?: FormPresentation; activeSection?: string; onSectionChange?(id: string): void };

function PresentedObject({ presentation, activeSection, onSectionChange, ...props }: PresentedObjectProps) {
  const i18n = usePortalI18n();
  const compactRoot = props.fieldPathId.path.length === 0 && presentation?.layout === "compact";
  if (props.fieldPathId.path.length !== 0 || presentation?.sections === undefined || presentation.sections.length === 0) return <section>{compactRoot || props.title === "" ? null : <Typography.Title level={5}>{props.title}</Typography.Title>}{props.description}{props.properties.filter((property) => !property.hidden).map((property) => <div key={property.name}>{property.content}</div>)}</section>;
  const sections = presentation.sections;
  const selected = sections.find((section) => section.id === activeSection) ?? sections[0]!;
  const assigned = new Set(sections.flatMap((section) => section.fields.map(formFieldName)));
  const remainder = props.properties.filter((property) => !assigned.has(property.name) && !property.hidden);
  const renderSection = (section: FormSectionPresentation) => {
    const fields = section.fields.map(formFieldName);
    const body = <div style={{ display: "grid", gridTemplateColumns: `repeat(${section.columns ?? 1}, minmax(0, 1fr))`, gap: 16 }}>{props.properties.filter((property) => fields.includes(property.name) && !property.hidden).map((property) => {
      const span = presentation.fields?.find((field) => formFieldName(field.pointer) === property.name)?.span ?? 1;
      return <div key={property.name} style={{ gridColumn: `span ${Math.min(Math.max(1, span), section.columns ?? 1)}` }}>{property.content}</div>;
    })}</div>;
    const description = section.description === undefined ? null : <Typography.Paragraph type="secondary">{i18n.text(section.description)}</Typography.Paragraph>;
    if (presentation.navigation !== "sections") return <>{description}{body}</>;
    return section.collapsible
      ? <details><summary>{section.title === undefined ? section.id : i18n.text(section.title)}</summary>{description}{body}</details>
      : <Card title={section.title === undefined ? undefined : i18n.text(section.title)}>{description}{body}</Card>;
  };
  const remaining = remainder.length === 0 ? null : <div>{remainder.map((property) => <div key={property.name}>{property.content}</div>)}</div>;
  if (presentation.navigation === "tabs") return <><Tabs activeKey={selected.id} onChange={onSectionChange} items={sections.map((section) => ({ key: section.id, label: section.title === undefined ? section.id : i18n.text(section.title), children: renderSection(section) }))} />{remaining}</>;
  if (presentation.navigation === "steps") {
    const current = Math.max(0, sections.findIndex((section) => section.id === selected.id));
    return <><Steps current={current} onChange={(index) => onSectionChange?.(sections[index]!.id)} items={sections.map((section, index) => ({ title: section.title === undefined ? `${index + 1}` : i18n.text(section.title) }))} /><div style={{ marginTop: 24 }}>{renderSection(selected)}</div>{remaining}</>;
  }
  return <div style={{ display: "grid", gap: 16 }}>{sections.map((section) => <div key={section.id}>{renderSection(section)}</div>)}{remaining}</div>;
}

export function FormRenderer({ schema, value, onChange, presentation, presentationSection, onPresentationSectionChange, readOnly, submitting, errors: externalErrors = {}, context: suppliedContext, validate, validationDelayMs = 250, onValidationChange }: FormRendererProps) {
  const i18n = usePortalI18n();
  const formContext = suppliedContext ?? emptyFormContext;
  const localizedSchema = useMemo(() => localizeJSONSchema(schema.schema, schema.localization, i18n.text), [i18n.text, schema.localization, schema.schema]);
  const localizedUISchema = useMemo(() => schema.uiSchema === undefined ? undefined : localizeJSONSchema(schema.uiSchema, schema.uiLocalization, i18n.text), [i18n.text, schema.uiLocalization, schema.uiSchema]);
  const validation = useMemo(() => validator.validateFormData(value, schema.schema), [schema.schema, value]);
  const syncErrors = useMemo(() => Object.fromEntries(validation.errors.map((error, index) => [errorPath(error) || `form.${index}`, error.message ?? i18n.text(message(namespace, "form.invalid", "值不符合 Schema"))])), [i18n, validation.errors]);
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
  const templates = useMemo(() => ({ ...safeAntdTemplates, ObjectFieldTemplate: (props: ObjectFieldTemplateProps) => <PresentedObject {...props} presentation={presentation} activeSection={presentationSection} onSectionChange={onPresentationSectionChange} />, ButtonTemplates: { ...safeAntdTemplates.ButtonTemplates, SubmitButton: () => null } }), [onPresentationSectionChange, presentation, presentationSection]);
  const widgets = useMemo(() => ({ ...antdWidgets, secretRef: SecretRefWidget }), []);
  const compact = presentation?.layout === "compact";
  const form = <RJSFForm
    schema={localizedSchema}
    uiSchema={localizedUISchema}
    formData={value}
    validator={validator}
    readonly={readOnly}
    disabled={submitting}
    liveValidate="onChange"
    showErrorList="top"
    extraErrors={errorSchema(combinedExternalErrors) as never}
    extraErrorsBlockSubmit
    noHtml5Validate
    formContext={formContext}
    onChange={(event) => onChange((event.formData ?? {}) as Record<string, unknown>)}
    templates={templates}
    widgets={widgets}
  ><></></RJSFForm>;
  return compact
    ? <ConfigProvider componentSize="small" theme={compactFormTheme}><div>{form}</div></ConfigProvider>
    : <div style={presentation?.layout === "horizontal" ? { display: "block" } : undefined}>{form}</div>;
}

function formFieldName(pointer: string): string {
  const first = pointer.startsWith("/") ? pointer.slice(1).split("/")[0] ?? "" : pointer;
  return first.replace(/~1/g, "/").replace(/~0/g, "~");
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
