import { Input, InputNumber, Typography } from "antd";
import type { BaseInputTemplateProps, DescriptionFieldProps, FieldHelpProps, TemplatesType } from "@rjsf/utils";
import { ariaDescribedByIds, getInputProps, helpId } from "@rjsf/utils";
import ArrayFieldItemTemplate from "@rjsf/antd/lib/templates/ArrayFieldItemTemplate/index.js";
import ArrayFieldTemplate from "@rjsf/antd/lib/templates/ArrayFieldTemplate/index.js";
import CyclicSchemaExpandTemplate from "@rjsf/antd/lib/templates/CyclicSchemaExpandTemplate/index.js";
import ErrorListTemplate from "@rjsf/antd/lib/templates/ErrorList/index.js";
import FieldErrorTemplate from "@rjsf/antd/lib/templates/FieldErrorTemplate/index.js";
import FieldTemplate from "@rjsf/antd/lib/templates/FieldTemplate/index.js";
import GridTemplate from "@rjsf/antd/lib/templates/GridTemplate/index.js";
import { AddButton, ClearButton, CopyButton, MoveDownButton, MoveUpButton, RemoveButton } from "@rjsf/antd/lib/templates/IconButton/index.js";
import MultiSchemaFieldTemplate from "@rjsf/antd/lib/templates/MultiSchemaFieldTemplate/index.js";
import OptionalDataControlsTemplate from "@rjsf/antd/lib/templates/OptionalDataControlsTemplate/index.js";
import TitleFieldTemplate from "@rjsf/antd/lib/templates/TitleField/index.js";
import WrapIfAdditionalTemplate from "@rjsf/antd/lib/templates/WrapIfAdditionalTemplate/index.js";

export const safeAntdTemplates: Partial<TemplatesType> = {
  ArrayFieldItemTemplate,
  ArrayFieldTemplate,
  BaseInputTemplate,
  CyclicSchemaExpandTemplate,
  ButtonTemplates: { AddButton, ClearButton, CopyButton, MoveDownButton, MoveUpButton, RemoveButton, SubmitButton: () => null },
  DescriptionFieldTemplate,
  ErrorListTemplate,
  FieldErrorTemplate,
  FieldHelpTemplate,
  FieldTemplate,
  GridTemplate,
  MultiSchemaFieldTemplate,
  OptionalDataControlsTemplate,
  TitleFieldTemplate,
  WrapIfAdditionalTemplate,
};

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
