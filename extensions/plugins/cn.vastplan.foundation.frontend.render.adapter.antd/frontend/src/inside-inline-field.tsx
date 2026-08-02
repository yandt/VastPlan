import { Form, Tooltip, Typography } from "antd";
import type { FieldTemplateProps } from "@rjsf/utils";
import type { FormLabelPlacement } from "@vastplan/ui-primitives";
import { AntdFieldTemplate } from "./safe-rjsf-theme";

export function PresentedField(props: FieldTemplateProps & { placement?: FormLabelPlacement }) {
  if (props.hidden || props.schema.type === "object" || props.schema.type === "array" || props.placement === "stacked") return <AntdFieldTemplate {...props} />;
  const label = props.displayLabel === false ? "" : props.label;
  const error = props.rawErrors?.[0];
  if (props.placement === "inline") return <Form.Item
    label={label === "" ? undefined : label}
    required={props.required}
    extra={props.rawHelp ?? props.rawDescription}
    validateStatus={(props.rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={null}
    labelCol={{ flex: "0 0 112px" }}
    wrapperCol={{ flex: "1 1 0", style: { minWidth: 0 } }}
    colon={false}
    style={{ marginBottom: 16 }}
  ><div>{props.children}{error === undefined ? null : <Typography.Text id={`${props.id}__error`} type="danger" role="alert" style={{ display: "block", marginTop: 4 }}>{error}</Typography.Text>}</div></Form.Item>;
  return <Form.Item
    required={props.required}
    extra={props.rawHelp ?? props.rawDescription}
    validateStatus={(props.rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={null}
    style={{ marginBottom: 0 }}
  ><div className="vp-antd-inside-inline-field">
    {label === "" ? null : <Tooltip title={label}><label className="vp-inside-inline-label" htmlFor={props.id} aria-label={label}>{label}{props.required ? <span aria-hidden style={{ color: "var(--ant-color-error)" }}> *</span> : null}</label></Tooltip>}
    <div className="vp-inside-inline-control">{props.children}</div>
  </div>{error === undefined ? null : <Typography.Text id={`${props.id}__error`} type="danger" role="alert" style={{ display: "block", marginTop: 4 }}>{error}</Typography.Text>}</Form.Item>;
}

export const antdInsideInlineCSS = `
.vp-antd-inside-inline-field{box-sizing:border-box;width:100%;min-width:0;min-height:32px;display:flex;align-items:center;border:1px solid var(--ant-color-border);border-radius:var(--ant-border-radius);background:var(--ant-color-bg-container);transition:border-color .15s,box-shadow .15s}
.vp-antd-inside-inline-field:focus-within{border-color:var(--ant-color-primary);box-shadow:0 0 0 2px color-mix(in srgb,var(--ant-color-primary) 10%,transparent)}
.vp-antd-inside-inline-field .vp-inside-inline-label{box-sizing:border-box;flex:0 1 auto;max-width:clamp(48px,18%,112px);min-width:0;padding:0 6px;color:var(--ant-color-text-secondary);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;border-right:1px solid var(--ant-color-border-secondary);cursor:default}
.vp-antd-inside-inline-field .vp-inside-inline-control{flex:1;min-width:0}
.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-input,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-input-affix-wrapper,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-input-number,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-picker,.vp-antd-inside-inline-field .vp-inside-inline-control>.ant-select{width:100%;border:0!important;box-shadow:none!important;background:transparent!important}
@media (max-width:767px){.vp-antd-inside-inline-field .vp-inside-inline-label{max-width:clamp(56px,32%,128px)}}
`;

export const antdFormDialogCSS = `
.vp-antd-form-dialog .vp-antd-form-controls-start .ant-form-item-label,.vp-antd-form-dialog .vp-antd-form-controls-end .ant-form-item-label{flex:0 0 var(--vp-form-dialog-label-width,96px)!important;min-width:0}
.vp-antd-form-dialog .vp-antd-form-controls-start .ant-form-item-label>label,.vp-antd-form-dialog .vp-antd-form-controls-end .ant-form-item-label>label{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
`;
