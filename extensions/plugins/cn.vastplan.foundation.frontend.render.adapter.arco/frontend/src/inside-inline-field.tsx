import { createContext, useContext } from "react";
import type { FieldTemplateProps } from "@rjsf/utils";
import { Form, Tooltip, Typography } from "./arco-components";

export const CompactFormContext = createContext(false);
export const InsideInlineLabelContext = createContext(false);

export function ArcoFieldTemplate({ id, label, children, rawDescription, rawHelp, rawErrors, hidden, required, displayLabel, schema }: FieldTemplateProps) {
  const compact = useContext(CompactFormContext);
  const insideInline = useContext(InsideInlineLabelContext);
  if (hidden) return <div style={{ display: "none" }}>{children}</div>;
  if (schema.type === "object" || schema.type === "array") return <>
    {children}
    {rawHelp === undefined ? null : <Typography.Paragraph type="secondary">{rawHelp}</Typography.Paragraph>}
  </>;
  if (insideInline) return <Form.Item
    required={required}
    extra={rawHelp ?? rawDescription}
    validateStatus={(rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={rawErrors?.[0]}
    style={{ marginBottom: 0 }}
  ><div className="vp-arco-inside-inline-field">
    {displayLabel === false || label === "" ? null : <Tooltip content={label}><label className="vp-inside-inline-label" htmlFor={id} aria-label={label}>{label}{required ? <span aria-hidden style={{ color: "rgb(var(--danger-6))" }}> *</span> : null}</label></Tooltip>}
    <div className="vp-inside-inline-control">{children}</div>
  </div></Form.Item>;
  return <Form.Item
    label={displayLabel === false ? undefined : label}
    required={required}
    extra={rawHelp ?? rawDescription}
    validateStatus={(rawErrors?.length ?? 0) > 0 ? "error" : undefined}
    help={rawErrors?.[0]}
    style={compact ? { marginBottom: 0 } : undefined}
  >{children}</Form.Item>;
}

export const arcoInsideInlineCSS = `
.vp-arco-inside-inline-field{box-sizing:border-box;width:100%;min-width:0;min-height:32px;display:flex;align-items:center;border:1px solid var(--color-border-2);border-radius:var(--border-radius-small);background:var(--color-bg-2);transition:border-color .15s,box-shadow .15s}
.vp-arco-inside-inline-field:focus-within{border-color:rgb(var(--primary-6));box-shadow:0 0 0 2px var(--color-primary-light-2)}
.vp-arco-inside-inline-field .vp-inside-inline-label{box-sizing:border-box;flex:0 1 auto;max-width:40%;min-width:0;padding:0 8px;color:var(--color-text-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;border-right:1px solid var(--color-border-2);cursor:default}
.vp-arco-inside-inline-field .vp-inside-inline-control{flex:1;min-width:0}
.vp-arco-inside-inline-field .vp-inside-inline-control>.arco-input-wrapper,.vp-arco-inside-inline-field .vp-inside-inline-control>.arco-input-number,.vp-arco-inside-inline-field .vp-inside-inline-control>.arco-picker,.vp-arco-inside-inline-field .vp-inside-inline-control>.arco-select .arco-select-view{width:100%;border:0!important;box-shadow:none!important;background:transparent!important}
@media (max-width:767px){.vp-arco-inside-inline-field .vp-inside-inline-label{max-width:45%}}
`;
