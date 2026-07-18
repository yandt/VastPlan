import type { FormField, FormSchema, FormValidationIssue, FormValidationResult } from "./index.js";

type FormValue = Record<string, unknown>;

function pathParts(path: string): string[] {
  return path.replace(/\[(\d+)\]/g, ".$1").split(".").filter(Boolean);
}

export function getFormValue(value: unknown, path: string): unknown {
  return pathParts(path).reduce<unknown>((current, key) => {
    if (Array.isArray(current)) return current[Number(key)];
    if (typeof current === "object" && current !== null) return (current as FormValue)[key];
    return undefined;
  }, value);
}

export function isFormFieldVisible(field: FormField, rootValue: FormValue): boolean {
  if (field.visibleWhen === undefined) return true;
  const current = getFormValue(rootValue, field.visibleWhen.key);
  if (field.visibleWhen.equals !== undefined) return Object.is(current, field.visibleWhen.equals);
  if (field.visibleWhen.notEquals !== undefined) return !Object.is(current, field.visibleWhen.notEquals);
  return Boolean(current);
}

function empty(value: unknown): boolean {
  return value === undefined || value === null || value === "" || (Array.isArray(value) && value.length === 0);
}

function measuredLength(value: unknown): number | undefined {
  if (typeof value === "string" || Array.isArray(value)) return value.length;
  return undefined;
}

function issue(path: string, code: FormValidationIssue["code"], field: FormField, limit?: number): FormValidationIssue {
  return { path, code, ...(field.validation?.message === undefined ? {} : { message: field.validation.message }), ...(limit === undefined ? {} : { limit }) };
}

function validateRules(field: FormField, value: unknown, path: string): FormValidationIssue[] {
  const rules = field.validation;
  if (rules === undefined) return [];
  if (rules.required && empty(value)) return [issue(path, "required", field)];
  if (empty(value)) return [];

  const issues: FormValidationIssue[] = [];
  const length = measuredLength(value);
  if (rules.min !== undefined) {
    const tooSmall = typeof value === "number" ? value < rules.min : length !== undefined && length < rules.min;
    if (tooSmall) issues.push(issue(path, "min", field, rules.min));
  }
  if (rules.max !== undefined) {
    const tooLarge = typeof value === "number" ? value > rules.max : length !== undefined && length > rules.max;
    if (tooLarge) issues.push(issue(path, "max", field, rules.max));
  }
  if (rules.pattern !== undefined && typeof value === "string") {
    try {
      if (!new RegExp(rules.pattern).test(value)) issues.push(issue(path, "pattern", field));
    } catch {
      issues.push(issue(path, "invalidPattern", field));
    }
  }
  return issues;
}

function childPath(parent: string, key: string): string {
  return parent === "" ? key : `${parent}.${key}`;
}

function validateFields(fields: FormField[], value: FormValue, rootValue: FormValue, parent = ""): FormValidationIssue[] {
  return fields.flatMap((field) => {
    if (!isFormFieldVisible(field, rootValue)) return [];
    const path = childPath(parent, field.key);
    const current = value[field.key];
    const issues = validateRules(field, current, path);
    if (field.type === "object" && field.fields !== undefined && typeof current === "object" && current !== null && !Array.isArray(current)) {
      issues.push(...validateFields(field.fields, current as FormValue, rootValue, path));
    }
    if (field.type === "array" && field.fields !== undefined && Array.isArray(current)) {
      current.forEach((entry, index) => {
        if (typeof entry === "object" && entry !== null && !Array.isArray(entry)) {
          issues.push(...validateFields(field.fields ?? [], entry as FormValue, rootValue, `${path}[${index}]`));
        }
      });
    }
    return issues;
  });
}

export function validateForm(schema: FormSchema, value: FormValue): FormValidationResult {
  const issues = validateFields(schema.fields, value, value);
  return { valid: issues.length === 0, issues };
}

function defaultForField(field: FormField): unknown {
  if (field.defaultValue !== undefined) return field.defaultValue;
  if (field.type === "object") return applyDefaultsToFields(field.fields ?? [], {});
  if (field.type === "array" || field.type === "multiSelect") return [];
  if (field.type === "boolean") return false;
  return undefined;
}

function applyDefaultsToFields(fields: FormField[], value: FormValue): FormValue {
  let next = value;
  const assign = (key: string, entry: unknown) => {
    if (next === value) next = { ...value };
    next[key] = entry;
  };
  for (const field of fields) {
    const current = next[field.key];
    if (current === undefined) {
      const initial = defaultForField(field);
      if (initial !== undefined) assign(field.key, initial);
    } else if (field.type === "object" && field.fields !== undefined && typeof current === "object" && current !== null && !Array.isArray(current)) {
      const nested = applyDefaultsToFields(field.fields, current as FormValue);
      if (nested !== current) assign(field.key, nested);
    } else if (field.type === "array" && field.fields !== undefined && Array.isArray(current)) {
      const entries = current.map((entry) => typeof entry === "object" && entry !== null && !Array.isArray(entry)
        ? applyDefaultsToFields(field.fields ?? [], entry as FormValue)
        : entry);
      if (entries.some((entry, index) => entry !== current[index])) assign(field.key, entries);
    }
  }
  return next;
}

export function applyFormDefaults(schema: FormSchema, value: FormValue): FormValue {
  return applyDefaultsToFields(schema.fields, value);
}
