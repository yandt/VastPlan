import { InputNumber, Select } from "antd";
import type { WidgetProps } from "@rjsf/utils";
import { durationUnits, message, usePortalI18n, type DurationUnit, type FormDurationPresentation } from "@vastplan/ui-primitives";
import { useEffect, useState } from "react";
import { namespace } from "./theme";

const millisecondFactors: Readonly<Record<DurationUnit, number>> = Object.freeze({
  millisecond: 1,
  second: 1_000,
  minute: 60_000,
  hour: 3_600_000,
  day: 86_400_000,
  week: 604_800_000,
  month: 2_592_000_000,
});

export function DurationWidget({ id, value, disabled, readonly, required, options, schema, label, onChange, onBlur, onFocus }: WidgetProps) {
  const i18n = usePortalI18n();
  const config = durationConfiguration(options.vastplanDuration);
  const preferred = config.defaultUnit ?? config.units[0]!;
  const [unit, setUnit] = useState<DurationUnit>(preferred);
  useEffect(() => { if (!config.units.includes(unit)) setUnit(preferred); }, [config.units, preferred, unit]);
  const numeric = finiteNumber(value);
  const displayed = numeric === undefined ? undefined : durationDisplayValue(numeric, config.storageUnit, unit);
  const minimum = finiteNumber(schema.minimum);
  const maximum = finiteNumber(schema.maximum);
  return <div className="vp-antd-duration-input" data-disabled={disabled || readonly || undefined}><InputNumber
    id={id}
    value={displayed}
    disabled={disabled}
    readOnly={readonly}
    required={required}
    aria-label={label}
    variant="borderless"
    min={minimum === undefined ? undefined : durationDisplayValue(minimum, config.storageUnit, unit)}
    max={maximum === undefined ? undefined : durationDisplayValue(maximum, config.storageUnit, unit)}
    step={unit === "millisecond" ? 1 : 0.1}
    onChange={(next) => {
      if (next === null) { onChange(options.emptyValue); return; }
      const stored = durationStorageValue(Number(next), config.storageUnit, unit);
      onChange(schema.type === "integer" ? Math.round(stored) : stored);
    }}
    onBlur={() => onBlur(id, value)}
    onFocus={() => onFocus(id, value)}
  /><Select
    aria-label={i18n.text(message(namespace, "form.duration.unit", "时间单位"))}
    value={unit}
    disabled={disabled || readonly}
    variant="borderless"
    popupMatchSelectWidth={false}
    options={config.units.map((candidate) => ({ value: candidate, label: i18n.text(durationUnitLabel(candidate)) }))}
    onChange={(next: DurationUnit) => setUnit(next)}
  /></div>;
}

export const antdDurationWidgetCSS = `
.vp-antd-duration-input{box-sizing:border-box;display:flex;align-items:stretch;width:100%;min-width:0;border:1px solid var(--ant-color-border);border-radius:var(--ant-border-radius);background:var(--ant-color-bg-container);overflow:hidden;transition:border-color .15s,box-shadow .15s}
.vp-antd-duration-input:hover:not([data-disabled="true"]){border-color:var(--ant-color-primary-hover)}
.vp-antd-duration-input:focus-within{border-color:var(--ant-color-primary);box-shadow:0 0 0 2px color-mix(in srgb,var(--ant-color-primary) 10%,transparent)}
.vp-antd-duration-input[data-disabled="true"]{background:var(--ant-color-bg-container-disabled);cursor:not-allowed}
.ant-form-item-has-error .vp-antd-duration-input{border-color:var(--ant-color-error)}
.ant-form-item-has-error .vp-antd-duration-input:focus-within{box-shadow:0 0 0 2px color-mix(in srgb,var(--ant-color-error) 10%,transparent)}
.vp-antd-duration-input>.ant-input-number{flex:1 1 auto;width:0;min-width:0;border:0!important;border-radius:0;box-shadow:none!important;background:transparent!important}
.vp-antd-duration-input>.ant-select{flex:0 0 88px;width:88px;min-width:0}
`;

/** Conversion does not mutate the stored value when only the display unit changes. */
export function durationDisplayValue(value: number, storageUnit: DurationUnit, displayUnit: DurationUnit): number {
  return boundedPrecision(value * millisecondFactors[storageUnit] / millisecondFactors[displayUnit]);
}

export function durationStorageValue(value: number, storageUnit: DurationUnit, displayUnit: DurationUnit): number {
  return boundedPrecision(value * millisecondFactors[displayUnit] / millisecondFactors[storageUnit]);
}

function durationConfiguration(value: unknown): FormDurationPresentation {
  if (typeof value !== "object" || value === null) throw new Error("Duration widget 缺少单位配置");
  const candidate = value as Partial<FormDurationPresentation>;
  const allowed = new Set(durationUnits);
  if (!allowed.has(candidate.storageUnit as DurationUnit) || !Array.isArray(candidate.units) || candidate.units.length === 0 ||
      candidate.units.length > durationUnits.length || new Set(candidate.units).size !== candidate.units.length ||
      candidate.units.some((unit) => !allowed.has(unit)) || candidate.defaultUnit !== undefined && !candidate.units.includes(candidate.defaultUnit)) {
    throw new Error("Duration widget 单位配置无效");
  }
  return candidate as FormDurationPresentation;
}

function durationUnitLabel(unit: DurationUnit) {
  return ({
    millisecond: message(namespace, "form.duration.millisecond", "毫秒"),
    second: message(namespace, "form.duration.second", "秒"),
    minute: message(namespace, "form.duration.minute", "分"),
    hour: message(namespace, "form.duration.hour", "小时"),
    day: message(namespace, "form.duration.day", "天"),
    week: message(namespace, "form.duration.week", "周"),
    month: message(namespace, "form.duration.month", "月"),
  } as const)[unit];
}

function finiteNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function boundedPrecision(value: number): number {
  return Number(value.toFixed(9));
}
