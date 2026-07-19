import {
  createErrorHandler,
  toErrorList,
  toErrorSchema,
  unwrapErrorHandler,
} from "@rjsf/utils";
import type {
  CustomValidator,
  ErrorTransformer,
  RJSFSchema,
  RJSFValidationError,
  UiSchema,
  ValidationData,
  ValidatorType,
} from "@rjsf/utils";
import {
  compileSchema,
  draft07,
  extendDraft,
} from "json-schema-library";
import type { JsonError, JsonSchema, SchemaNode } from "json-schema-library";

type FormData = Record<string, unknown>;
type FormContext = Readonly<Record<string, unknown>>;
type Schema = RJSFSchema;

const credentialReference = /^credential:\/\/[A-Za-z0-9][A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*$/;

const vastplanDraft07 = extendDraft(draft07, {
  formats: {
    "vastplan-credential-ref": ({ data, node, pointer }) => {
      if (typeof data === "string" && credentialReference.test(data)) return;
      return node.createError("vastplan-credential-ref-error", {
        pointer,
        schema: node.schema,
        value: data,
      });
    },
  },
  errors: {
    "vastplan-credential-ref-error": "Value at `{{pointer}}` must be a credential:// reference",
  },
});

const compiled = new WeakMap<object, SchemaNode>();

function compile(schema: Schema): SchemaNode {
  const key = schema as object;
  const existing = compiled.get(key);
  if (existing !== undefined) return existing;
  const node = compileSchema(schema as JsonSchema, {
    drafts: [vastplanDraft07],
    formatAssertion: true,
    throwOnInvalidRef: true,
    throwOnInvalidSchema: true,
  });
  compiled.set(key, node);
  return node;
}

function pointerProperty(pointer: unknown): string {
  if (typeof pointer !== "string" || pointer === "" || pointer === "#") return "";
  const parts = pointer.replace(/^#\/?/, "").split("/").filter(Boolean).map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"));
  return parts.map((part) => /^\d+$/.test(part)
    ? `[${part}]`
    : /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(part) ? `.${part}` : `['${part.replace(/'/g, "\\'")}']`).join("");
}

function errorName(code: string): string {
  switch (code) {
    case "required-property-error": return "required";
    case "no-additional-properties-error": return "additionalProperties";
    case "min-length-error": return "minLength";
    case "max-length-error": return "maxLength";
    case "minimum-error": return "minimum";
    case "maximum-error": return "maximum";
    case "exclusive-minimum-error": return "exclusiveMinimum";
    case "exclusive-maximum-error": return "exclusiveMaximum";
    case "min-items-error": return "minItems";
    case "max-items-error": return "maxItems";
    case "min-properties-error": return "minProperties";
    case "max-properties-error": return "maxProperties";
    case "pattern-error": return "pattern";
    case "vastplan-credential-ref-error": return "format";
    default: return code.replace(/-error$/, "").replace(/-([a-z])/g, (_, letter: string) => letter.toUpperCase());
  }
}

function errorParams(error: JsonError): Record<string, unknown> {
  const data = error.data ?? {};
  const name = errorName(typeof error.code === "string" ? error.code : "schema-error");
  switch (name) {
    case "required": return { missingProperty: data.key };
    case "additionalProperties": return { additionalProperty: data.property };
    case "minLength": return { limit: data.minLength };
    case "maxLength": return { limit: data.maxLength };
    case "minimum": return { limit: data.minimum };
    case "maximum": return { limit: data.maximum };
    case "exclusiveMinimum": return { limit: data.exclusiveMinimum };
    case "exclusiveMaximum": return { limit: data.exclusiveMaximum };
    case "minItems": return { limit: data.minItems };
    case "maxItems": return { limit: data.maxItems };
    case "minProperties": return { limit: data.minProperties };
    case "maxProperties": return { limit: data.maxProperties };
    case "pattern": return { pattern: data.pattern };
    case "format": return { format: data.format ?? "vastplan-credential-ref" };
    default: return {};
  }
}

function toRJSFError(error: JsonError): RJSFValidationError {
  const property = pointerProperty(error.data?.pointer);
  return {
    name: errorName(typeof error.code === "string" ? error.code : "schema-error"),
    property,
    message: error.message,
    params: errorParams(error),
    stack: `${property} ${error.message}`.trim(),
  };
}

function pureValidation(schema: Schema, formData: FormData | undefined): RJSFValidationError[] {
  return compile(schema).validate(formData).errors.map(toRJSFError);
}

function validateFormData(
  formData: FormData | undefined,
  schema: Schema,
  customValidate?: CustomValidator<FormData, Schema, FormContext>,
  transformErrors?: ErrorTransformer<FormData, Schema, FormContext>,
  uiSchema?: UiSchema<FormData, Schema, FormContext>,
): ValidationData<FormData> {
  let errors = pureValidation(schema, formData);
  if (customValidate !== undefined) {
    const handler = customValidate(formData, createErrorHandler(formData ?? {}), uiSchema);
    errors = [...errors, ...toErrorList(unwrapErrorHandler(handler))];
  }
  if (transformErrors !== undefined) errors = transformErrors(errors, uiSchema);
  return { errors, errorSchema: toErrorSchema(errors) };
}

export const cspJSONSchemaValidator: ValidatorType<FormData, Schema, FormContext> = {
  validateFormData,
  isValid(schema, formData, rootSchema) {
    const candidate = schema === rootSchema || typeof schema !== "object"
      ? schema
      : { ...schema, definitions: schema.definitions ?? rootSchema.definitions, $defs: schema.$defs ?? rootSchema.$defs };
    try { return pureValidation(candidate, formData).length === 0; }
    catch { return false; }
  },
  rawValidation<Result = RJSFValidationError>(schema: Schema, formData?: FormData): { errors?: Result[]; validationError?: Error } {
    try { return { errors: pureValidation(schema, formData) as unknown as Result[] }; }
    catch (error) { return { validationError: error instanceof Error ? error : new Error("Schema 校验失败") }; }
  },
};
