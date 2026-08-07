import { ExclamationCircleFilled, QuestionCircleOutlined } from "@ant-design/icons";
import { Form, Tooltip } from "antd";
import type { FieldTemplateProps } from "@rjsf/utils";
import type { ReactNode } from "react";
import type { FormLabelPlacement } from "@vastplan/ui-primitives";
import { AntdFieldTemplate } from "./safe-rjsf-theme";

export function PresentedField(props: FieldTemplateProps & { placement?: FormLabelPlacement }) {
  if (props.hidden || props.schema.type === "object") return <AntdFieldTemplate {...props} />;
  const arrayField = props.schema.type === "array";
  // RJSF suppresses array labels by default; only the generated item labels should be hidden.
  const label = arrayField ? props.label : props.displayLabel === false ? "" : props.label;
  const booleanField = props.schema.type === "boolean";
  const description = hasContent(props.rawDescription) ? props.rawDescription : undefined;
  const labelContent = label === "" ? undefined : <FieldLabel label={label} description={description} />;
  const extra = booleanField ? props.rawHelp ?? props.rawDescription : props.rawHelp;
  const labelColumnWidth = "min(var(--vp-form-label-width,var(--vp-form-label-min-width,112px)),48%)";
  if (arrayField) return <Form.Item
    className="vp-antd-form-field-value vp-antd-form-field-array"
    label={labelContent}
    required={props.required}
    extra={props.rawHelp}
    validateStatus={(props.rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={null}
    labelCol={props.placement === "stacked" ? undefined : { flex: "0 0 112px" }}
    wrapperCol={props.placement === "stacked" ? undefined : { flex: "1 1 0", style: { minWidth: 0 } }}
    colon={false}
    style={{ marginBottom: "var(--vp-form-item-margin-bottom, 16px)" }}
  ><FieldControl errors={props.rawErrors}>{props.children}</FieldControl></Form.Item>;
  const fieldClassName = booleanField ? "vp-antd-form-field-boolean" : "vp-antd-form-field-value";
  if (props.placement === "stacked") return <Form.Item
    className={fieldClassName}
    label={booleanField ? undefined : labelContent}
    required={booleanField ? false : props.required}
    extra={extra}
    validateStatus={(props.rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={null}
    colon={false}
    style={{ marginBottom: "var(--vp-form-item-margin-bottom, 16px)" }}
  ><FieldControl errors={props.rawErrors}>{props.children}</FieldControl></Form.Item>;
  if (props.placement === "inline") return <Form.Item
    className={fieldClassName}
    label={booleanField ? undefined : labelContent}
    required={booleanField ? false : props.required}
    extra={extra}
    validateStatus={(props.rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={null}
    labelCol={booleanField ? undefined : { flex: "0 0 112px" }}
    wrapperCol={booleanField
      ? { flex: "1 1 0", style: { minWidth: 0, marginInlineStart: labelColumnWidth } }
      : { flex: "1 1 0", style: { minWidth: 0 } }}
    colon={false}
    style={{ marginBottom: "var(--vp-form-item-margin-bottom, 16px)" }}
  ><FieldControl errors={props.rawErrors}>{props.children}</FieldControl></Form.Item>;
  return <Form.Item
    required={props.required}
    extra={extra}
    validateStatus={(props.rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={null}
    style={{ marginBottom: 0 }}
  ><FieldControl errors={props.rawErrors}><div className="vp-antd-inside-inline-field">
      {label === "" ? null : <label className="vp-inside-inline-label" htmlFor={props.id} aria-label={label}><FieldLabel label={label} description={description} truncate required={props.required} /></label>}
      <div className="vp-inside-inline-control">{props.children}</div>
    </div></FieldControl></Form.Item>;
}

function FieldLabel({ label, description, required = false, truncate = false }: { label: string; description?: ReactNode; required?: boolean; truncate?: boolean }) {
  const labelText = <span className="vp-antd-form-label-text">{label}</span>;
  return <span className="vp-antd-form-label-content">
    {truncate ? <Tooltip title={label}>{labelText}</Tooltip> : labelText}
    {required ? <span aria-hidden className="vp-antd-form-label-required">*</span> : null}
    {description === undefined ? null : <Tooltip title={description}><span
      className="vp-antd-form-label-help"
      role="img"
      tabIndex={0}
      aria-label={typeof description === "string" ? description : `${label} field description`}
    ><QuestionCircleOutlined aria-hidden="true" /></span></Tooltip>}
  </span>;
}

function hasContent(value: ReactNode): boolean {
  return value !== undefined && value !== null && value !== false && value !== "";
}

function FieldControl({ children, errors }: { children: ReactNode; errors?: readonly string[] }) {
  const messages = [...new Set(errors?.filter((error) => error.trim() !== "") ?? [])];
  return <div className="vp-antd-form-field-control">
    {children}
    {messages.length === 0 ? null : <Tooltip
      color="red"
      placement="topRight"
      title={messages.length === 1 ? messages[0] : <div className="vp-antd-field-error-tooltip">{messages.map((message) => <div key={message}>{message}</div>)}</div>}
    ><span
      className="vp-antd-field-error-indicator"
      role="img"
      tabIndex={0}
      aria-label={messages.join("；")}
      data-tooltip-color="red"
    ><ExclamationCircleFilled /></span></Tooltip>}
  </div>;
}

export const antdInsideInlineCSS = `
.vp-antd-inside-inline-field{box-sizing:border-box;width:100%;min-width:0;min-height:32px;display:flex;align-items:center;border:1px solid var(--ant-color-border);border-radius:var(--ant-border-radius);background:var(--ant-color-bg-container);transition:border-color .15s,box-shadow .15s}
.vp-antd-inside-inline-field:focus-within{border-color:var(--ant-color-primary);box-shadow:0 0 0 2px color-mix(in srgb,var(--ant-color-primary) 10%,transparent)}
.vp-antd-inside-inline-field .vp-inside-inline-label{box-sizing:border-box;display:flex;align-items:center;flex:0 1 auto;max-width:clamp(48px,18%,112px);min-width:0;padding:0 6px;color:var(--ant-color-text-secondary);white-space:nowrap;overflow:visible;border-right:1px solid var(--ant-color-border-secondary);cursor:default}
.vp-antd-inside-inline-field .vp-inside-inline-label .vp-antd-form-label-content{width:100%}
.vp-antd-inside-inline-field .vp-inside-inline-control{flex:1;min-width:0}
.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-input,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-input-affix-wrapper,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-input-number,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-picker,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-select{width:100%;border:0!important;box-shadow:none!important;background:transparent!important}
@media (max-width:767px){.vp-antd-inside-inline-field .vp-inside-inline-label{max-width:clamp(56px,32%,128px)}}
`;

export const antdFormFieldWidthCSS = `
.vp-antd-form-field-value .ant-form-item-label{box-sizing:border-box;flex:0 0 min(var(--vp-form-label-width,var(--vp-form-label-min-width,112px)),48%)!important;width:min(var(--vp-form-label-width,var(--vp-form-label-min-width,112px)),48%);min-width:0;max-width:48%;padding-inline-end:12px}
.vp-antd-form-field-value .ant-form-item-label>label{display:inline-flex;align-items:center;white-space:nowrap;overflow:visible;text-overflow:clip}
.vp-antd-form-label-content{display:inline-flex;align-items:center;gap:4px;min-width:0;max-width:100%}
.vp-antd-form-label-text{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.vp-antd-form-label-required{flex:none;color:var(--ant-color-error)}
.vp-antd-form-label-help{display:inline-flex;align-items:center;justify-content:center;flex:none;color:var(--ant-color-text-tertiary);font-size:1em;line-height:1;cursor:help}
.vp-antd-form-label-help:hover,.vp-antd-form-label-help:focus-visible{color:var(--ant-color-primary)}
.vp-antd-form-label-help:focus-visible{outline:2px solid var(--ant-color-primary);outline-offset:2px;border-radius:50%}
.vp-antd-form-field-value .ant-form-item-control-input-content>div{width:100%;min-width:0}
.vp-antd-form-field-control{display:flex;align-items:center;gap:6px;width:100%;min-width:0}
.vp-antd-form-field-control>:first-child{flex:1 1 auto;min-width:0}
.vp-antd-field-error-indicator{display:inline-flex;align-items:center;justify-content:center;flex:none;color:var(--ant-color-error);font-size:1em;line-height:1;cursor:help}
.vp-antd-field-error-indicator:focus-visible{outline:2px solid var(--ant-color-error);outline-offset:2px;border-radius:50%}
.vp-antd-field-error-tooltip{display:grid;gap:2px}
.vp-antd-form-field-boolean .ant-form-item-control-input-content{justify-content:flex-start!important}
.vp-antd-form-field-boolean .ant-form-item-control-input-content>div{width:auto}
@media(max-width:479px){
.vp-antd-form-field-value .ant-form-item-row{display:block}
.vp-antd-form-field-value .ant-form-item-label{box-sizing:border-box;width:100%!important;max-width:100%;height:auto;min-height:0;flex:none!important;padding:0 0 4px;text-align:start}
.vp-antd-form-field-value .ant-form-item-label>label{height:auto;white-space:normal}
.vp-antd-form-field-value .ant-form-item-control{box-sizing:border-box;width:100%;max-width:100%}
.vp-antd-form-field-boolean .ant-form-item-control{margin-inline-start:0!important}
}
`;
