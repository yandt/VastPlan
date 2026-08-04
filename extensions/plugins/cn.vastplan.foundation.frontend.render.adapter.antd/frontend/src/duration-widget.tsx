import { InputNumber, Select, Space } from "antd";
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
  return <Space.Compact block style={{ width: "100%" }}><InputNumber
    id={id}
    value={displayed}
    disabled={disabled}
    readOnly={readonly}
    required={required}
    aria-label={label}
    style={{ flex: "1 1 auto", minWidth: 0, width: 0 }}
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
      style={{ width: 88 }}
      options={config.units.map((candidate) => ({ value: candidate, label: i18n.text(durationUnitLabel(candidate)) }))}
      onChange={(next: DurationUnit) => setUnit(next)}
    /></Space.Compact>;
}

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
